package hermes

import (
	"errors"
	"net/http"
	"time"

	"accur8.io/godev/a8nats"
	"accur8.io/godev/hermes/api"
	"accur8.io/godev/hermes/hproto"
	"accur8.io/godev/hermes/model"
	"accur8.io/godev/log"
	"accur8.io/godev/nefario"
	"accur8.io/godev/nefario/rpc"
	"github.com/gorilla/mux"
	"github.com/nats-io/nats.go"
	"github.com/palantir/stacktrace"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func BuildDownloadStreamService(services *Services, tail bool) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

		flusher, ok := w.(http.Flusher)
		if !ok {
			panic("expected http.ResponseWriter to be an http.Flusher")
		}

		w.Header().Set("Connection", "Keep-Alive")
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// cancelFn := func() {}
		ctx := r.Context()
		// if tail {
		// 	ctx, cancelFn = context.WithTimeout(ctx, 5*time.Minute)
		// }
		// defer cancelFn()

		vars := mux.Vars(r)
		processUid := vars["processUid"]
		channelName := vars["streamName"]
		startPos := vars["startPos"]

		if processUid == "" {
			http.Error(w, "missing processUid", http.StatusBadRequest)
			return
		}

		if channelName == "" {
			http.Error(w, "missing streamName", http.StatusBadRequest)
			return
		}

		log.Debug("Start DownloadStreamService() processUid - %v  stream - %v  tail - %v", processUid, channelName, tail)
		defer log.Debug("download complete")

		subject := nefario.ProcessSubjectName(processUid, channelName)
		streamName := nefario.ProcessStreamName(processUid, channelName)

		log.Debug("subject - %v  streamName - %v", subject, streamName)

		streamInfo, err := services.Nats().StreamInfo(streamName)
		if err != nil {
			http.Error(w, "stream not found", http.StatusNotFound)
			return
		}

		if !tail && streamInfo.State.FirstSeq == 0 && streamInfo.State.LastSeq == 0 {
			if log.IsTraceEnabled {
				log.Debug("stream is empty and we are downloading (not tailing) returning an empty 200 response immediately")
			}
			return
		}

		_, subOpt := a8nats.StartSeqToSubscriptionOptions(startPos)

		sub, err := services.Nats().JetStream().PullSubscribe(subject.String(), "", nats.BindStream(streamName.String()), subOpt)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		defer sub.Unsubscribe()

		fetchOpt := nats.MaxWait(2 * time.Second)

	outer:
		for {
			if log.IsTraceEnabled {
				log.Trace("fetching up to 100 messages")
			}
			msgs, err := sub.Fetch(100, fetchOpt)
			if err != nil {
				isTimeout := err.Error() == "nats: timeout" || errors.Is(err, nats.ErrTimeout)
				if isTimeout {
					if !tail {
						log.Debug("ending download because of timeout, this should not happen")
						break outer
					}
				} else {
					log.Debug("ending download because of error %v", err)
					break outer
				}
			}
			log.Debug("fetched %v messages", len(msgs))
			close := false
			for _, msg := range msgs {
				var record rpc.StreamRecord
				err := proto.Unmarshal(msg.Data, &record)
				if err != nil {
					log.Error(stacktrace.Propagate(err, "error Unmarshaling record from process stream").Error())
					continue
				}
				for _, buffer := range record.Buffers {
					w.Write(buffer.Data)
				}
				err = msg.Ack()
				if err != nil {
					log.Debug("error Ack'ing message %+v", msg)
				}
				metadata, _ := msg.Metadata()
				if metadata != nil {
					if metadata.Sequence.Stream >= streamInfo.State.LastSeq {
						close = true
					}
				}
			}
			if !tail && close {
				break outer
			}

			if tail {
				flusher.Flush()
			}

			select {
			case <-ctx.Done():
				log.Debug("ending download because request/timer context is done")
				break outer
			default:
			}

		}

		flusher.Flush()

	}
}

func BuildDownloadChannelService(services *Services) func(w http.ResponseWriter, r *http.Request) {

	return func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set("Content-Type", "application/json")

		vars := mux.Vars(r)
		adminKey := model.NewAdminKey(vars["adminKey"])
		channelName := model.NewChannelName(vars["channelName"])
		startPos := vars["startPos"]

		var toJsonFn func(msg *nats.Msg) []byte

		if channelName == api.RpcSent {
			toJsonFn = func(natsMsg *nats.Msg) []byte {
				var sr hproto.SendReceipt
				err := proto.Unmarshal(natsMsg.Data, &sr)
				if err != nil {
					panic(stacktrace.Propagate(err, "unable to unmarshal SendMessageRequest"))
				}
				md, err := natsMsg.Metadata()
				if err == nil {
					sr.ServerEnvelope = &hproto.ServerEnvelope{
						Created:  md.Timestamp.UnixMilli(),
						Sequence: md.Sequence.Stream,
					}
				}
				jsonStr, err := protojson.Marshal(&sr)
				if err != nil {
					panic(stacktrace.Propagate(err, "unable to marshal Message to json"))
				}
				return jsonStr
			}
		} else {
			toJsonFn = func(natsMsg *nats.Msg) []byte {
				var msg hproto.Message
				err := proto.Unmarshal(natsMsg.Data, &msg)
				if err != nil {
					panic(stacktrace.Propagate(err, "unable to unmarshal Message for MessageEnvelope.MessageBytes"))
				}
				metadata, err := natsMsg.Metadata()
				if err != nil {
					log.Error(stacktrace.Propagate(err, "unable to get metadata").Error())
				}
				msg.ServerEnvelope = &hproto.ServerEnvelope{
					Sequence: metadata.Sequence.Stream,
					Created:  metadata.Timestamp.UnixMilli(),
					Channel:  channelName.String(),
				}
				jsonStr, err := protojson.Marshal(&msg)
				if err != nil {
					panic(stacktrace.Propagate(err, "unable to marshal Message to json"))
				}
				return jsonStr
			}
		}

		if adminKey.IsEmpty() {
			http.Error(w, "missing adminKey", http.StatusBadRequest)
			return
		}

		if channelName.IsEmpty() {
			http.Error(w, "missing channelName", http.StatusBadRequest)
			return
		}

		log.Debug("Start DownloadChannelService() adminKey - %v  stream - %v  startPos - %v", adminKey, channelName, startPos)
		defer log.Debug("download complete")

		mbox := services.MailboxStore.FetchMailboxByAdminKey(adminKey)
		if mbox == nil {
			http.Error(w, "mbox not found", http.StatusNotFound)
			return
		}

		subject := mbox.NatsSubject(channelName)
		streamName := mbox.NatsStreamName(channelName)

		log.Debug("subject - %v  streamName - %v", subject, streamName)

		streamInfo, err := services.Nats().StreamInfo(streamName)
		if err != nil {
			http.Error(w, "stream not found", http.StatusNotFound)
			return
		}

		if streamInfo.State.FirstSeq == 0 && streamInfo.State.LastSeq == 0 {
			if log.IsTraceEnabled {
				log.Debug("stream is empty and we are downloading (not tailing) returning an empty 200 response immediately")
			}
			w.Write([]byte("[]"))
			return
		}

		_, subOpt := a8nats.StartSeqToSubscriptionOptions(startPos)

		sub, err := services.Nats().JetStream().PullSubscribe(subject.String(), "", nats.BindStream(streamName.String()), subOpt)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		defer sub.Unsubscribe()

		fetchOpt := nats.MaxWait(1 * time.Second)

		firstMsg := true
		w.Write([]byte("[\n  "))

	outer:
		for {
			if log.IsTraceEnabled {
				log.Trace("fetching up to 100 messages")
			}
			msgs, err := sub.Fetch(100, fetchOpt)
			if err != nil {
				isTimeout := err.Error() == "nats: timeout" || errors.Is(err, nats.ErrTimeout)
				if isTimeout {
					log.Debug("ending download because of timeout, this should not happen")
					break outer
				}
			}
			log.Debug("fetched %v messages", len(msgs))
			close := false
			for _, msg := range msgs {
				if firstMsg {
					firstMsg = false
				} else {
					w.Write([]byte("\n  ,"))
				}
				w.Write(toJsonFn(msg))
				err = msg.Ack()
				if err != nil {
					log.Debug("error Ack'ing message %+v", msg)
				}
				metadata, _ := msg.Metadata()
				if metadata != nil {
					if metadata.Sequence.Stream >= streamInfo.State.LastSeq {
						close = true
					}
				}
			}
			if close {
				break outer
			}
			select {
			case <-r.Context().Done():
				log.Debug("ending download because request context is done")
				break outer
			default:
			}

		}

		w.Write([]byte("\n]"))

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

	}
}

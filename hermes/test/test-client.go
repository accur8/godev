package main

import (
	"bytes"
	"context"
	"fmt"
	"time"

	base "accur8.io/godev/hermes/api"
	"accur8.io/godev/hermes/hproto"
	"accur8.io/godev/hermes/model"
	"accur8.io/godev/hermes/rpcclient"
	"accur8.io/godev/jsonrpc"
	"accur8.io/godev/log"
)

func NowUnixEpochMicro() int64 {
	return time.Now().UnixMicro()
}

func NowUnixEpochMilli() int64 {
	return time.Now().UnixMilli()
}

type Mailbox struct {
	AdminKey  string
	WriterKey string
	ReaderKey string
}

func main() {

	log.IsLoggingEnabled = true
	log.IsTraceEnabled = true
	log.InitFileLogging("./logs", "test-client")

	numberOfMessagesToProcessBeforeExit := int64(1_000_000)

	latencyTest := func() {

		rpcChannel := base.RpcInbox

		ctx := context.Background()

		// clientConfig := &rpcclient.RpcClientConfig{
		// 	HermesRootUrl: jsonrpc.UrlParse("https://hermes-grpc.ahsrcm.com:443"),
		// 	UseGrpc:       true,
		// 	RpcChannel:    rpcChannel,
		// 	GrpcEncrypt:   true,
		// 	Context:       ctx,
		// }

		clientConfig := &rpcclient.RpcClientConfig{
			HermesRootUrl: jsonrpc.UrlParse("https://hermes-go.ahsrcm.com:443"),
			UseGrpc:       false,
			RpcChannel:    rpcChannel,
			Context:       ctx,
			ExtraChannels: []model.ChannelName{base.RpcSent},
		}

		hermesClient, err := rpcclient.NewHermesClient(clientConfig)
		// hermesClient, err := rpcclient.NewHermesClient(ctx, jsonrpc.UrlParse("https://hermes-go.ahsrcm.com:8443"), rpcChannel, false)

		// hermesClient, err := rpcclient.NewHermesClient(ctx, jsonrpc.UrlParse("http://ahs-hermes.accur8.net:6081"), rpcChannel, true)
		// hermesClient, err := rpcclient.NewHermesClient(ctx, jsonrpc.UrlParse("http://ahs-hermes.accur8.net:6080"), rpcChannel, false)

		// hermesClient, err := rpcclient.NewHermesClient(ctx, jsonrpc.UrlParse("http://localhost:8081"), rpcChannel, true)
		// hermesClient, err := rpcclient.NewHermesClient(ctx, jsonrpc.UrlParse("http://localhost:8080"), rpcChannel, false)
		if err != nil {
			log.Error("error in NewTestHermesClient - %v", err)
			return
		}

		counter := int64(0)
		overallDelta := int64(0)
		wk := hermesClient.Client.ClientMailbox().Address
		to := []string{wk}

		roundtrip := func() error {

			messageBody := []byte(fmt.Sprintf("%v", counter+1000))

			smr := &hproto.SendMessageRequest{
				To:           to,
				Channel:      rpcChannel.String(),
				IdempotentId: model.RandomIdempotentId().String(),
				Message: &hproto.Message{
					Header: &hproto.MessageHeader{
						Sender: hermesClient.Client.ClientMailbox().Address,
					},
					Data: messageBody,
				},
			}

			counter += 1
			if log.IsTraceEnabled {
				log.Trace("sending message %v", counter)
			}
			start := NowUnixEpochMicro()
			err := hermesClient.SendMessageRequest(smr)
			if err != nil {
				return err
			}
			if log.IsTraceEnabled {
				log.Trace("message %v sent", counter)
			}
			msg, err := hermesClient.ReadMessage()
			if err != nil {
				return err
			}

			delta := NowUnixEpochMicro() - start
			overallDelta += delta
			log.Debug("received message %v delta %v", counter, delta)

			if !bytes.Equal(msg.Data, messageBody) {
				log.Debug("message body mismatch %v != %v", msg.Data, messageBody)
			}

			return nil

		}

		for {
			err := roundtrip()
			if err != nil {
				log.Error("received error - %v", err)
				break
			}
			if counter > numberOfMessagesToProcessBeforeExit {
				break
			}
		}

		log.Info("total round trips %d", counter)
		log.Info("average round trip time %d micros", overallDelta/counter)
		log.Info("roundtrip messages per second %d", (counter*1_000_000)/overallDelta)

	}

	// throughPutTest := func() {

	// 	log.Info("running throughput test")

	// 	writeCount := 0
	// 	readCount := 0

	// 	start := NowUnixEpochMilli()

	// 	writeMessages := func() {
	// 		for {
	// 			writeCount += 1
	// 			msg := fmt.Sprintf("%d", writeCount)
	// 			wsWriterConn.WriteMessage(websocket.TextMessage, []byte(msg))
	// 			if writeCount%100000 == 0 {
	// 				delta := NowUnixEpochMilli() - start
	// 				rate := writeCount * 1000 / int(delta)
	// 				log.Info("writeCount %d - %d", writeCount, rate)
	// 			}
	// 		}
	// 		// log.Printf("sending %s", msg)
	// 		// log.Printf("sent %s", msg)
	// 	}

	// 	readMessages := func() {
	// 		for {
	// 			// log.Println("reading messages")
	// 			_, data, err := wsReaderConn.ReadMessage()
	// 			if err != nil {
	// 				fmt.Println(fmt.Sprintf("error reading %s", err))
	// 				return
	// 			}
	// 			if false {
	// 				log.Info("message read %s", string(data))
	// 				delta := NowUnixEpochMilli() - start
	// 				fmt.Println(fmt.Sprintf("%d - %d micros", writeCount, delta))
	// 			}
	// 			readCount += 1
	// 			if readCount%100000 == 0 {
	// 				delta := NowUnixEpochMilli() - start
	// 				rate := readCount * 1000 / int(delta)
	// 				log.Info("readCount %d - %d", readCount, rate)
	// 			}
	// 		}
	// 	}
	// 	go readMessages()
	// 	go writeMessages()

	// 	for writeCount < 10000000 {
	// 		time.Sleep(10000000)
	// 	}
	// }

	// if true {
	// 	latencyTest()
	// } else {
	// 	throughPutTest()
	// }

	latencyTest()

}

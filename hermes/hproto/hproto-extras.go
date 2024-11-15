package hproto

import (
	"accur8.io/godev/hermes/model"
)

func (msg *Message) CorrelationId() string {
	hdr := msg.Header
	if hdr != nil {
		rh := hdr.RpcHeader
		if rh != nil {
			return rh.CorrelationId
		}
	}
	return ""
}
func (msg *Message) RpcFrameType() RpcFrameType {
	return msg.GetHeader().GetRpcHeader().GetFrameType()
}

func (rh *RpcHeader) CorrelationIdO() model.CorrelationId {
	return model.NewCorrelationId(rh.CorrelationId)
}

func (smr *SendMessageRequest) IdempotentIdO() model.IdempotentId {
	return model.NewIdempotentId(smr.IdempotentId)
}

func (smr *SendMessageRequest) ChannelO() model.ChannelName {
	return model.NewChannelName(smr.Channel)
}

func (ms *MailboxSubscription) ChannelO() model.ChannelName {
	return model.NewChannelName(ms.Channel)
}

func (ms *MailboxSubscription) ReaderKeyO() model.ReaderKey {
	return model.NewReaderKey(ms.ReaderKey)
}

func ToChannelNames(names []string) []model.ChannelName {
	arr := make([]model.ChannelName, len(names))
	for i, n := range names {
		arr[i] = model.NewChannelName(n)
	}
	return arr
}

func (acr *AddChannelRequest) ChannelNames() []model.ChannelName {
	return ToChannelNames(acr.Channels)
}

func (req *CreateMailboxRequest) ChannelNames() []model.ChannelName {
	return ToChannelNames(req.Channels)
}

func (si *SenderInfo) ReaderKeyO() model.ReaderKey {
	return model.NewReaderKey(si.ReaderKey)
}

func (si *SenderInfo) AddressO() model.MailboxAddress {
	return model.NewMailboxAddress(si.Address)
}

func (mh *MessageHeader) SenderO() model.MailboxAddress {
	return model.NewMailboxAddress(mh.Sender)
}

func (acr *AddChannelRequest) AdminKeyO() model.AdminKey {
	return model.NewAdminKey(acr.AdminKey)
}

func ToExtraHeaders(headers map[string]string) []*KeyValPair {
	arr := make([]*KeyValPair, len(headers))
	for k, v := range headers {
		arr = append(arr, &KeyValPair{Key: k, Val: v})
	}
	return arr
}

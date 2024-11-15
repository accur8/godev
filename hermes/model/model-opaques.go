package model

type ChannelName interface {
	String() string
	IsEmpty() bool
	implementsOpaque()
}

type _ChannelName string

func (value *_ChannelName) implementsOpaque() {}

func (value *_ChannelName) String() string {
	return string(*value)
}

func (value *_ChannelName) IsEmpty() bool {
	return len(string(*value)) == 0
}
func NewChannelName(s string) ChannelName {
	value := _ChannelName(s)
	return &value
}

type AdminKey interface {
	String() string
	IsEmpty() bool
	implementsOpaque()
}

type _AdminKey string

func (value *_AdminKey) implementsOpaque() {}

func (value *_AdminKey) String() string {
	return string(*value)
}

func (value *_AdminKey) IsEmpty() bool {
	return len(string(*value)) == 0
}
func NewAdminKey(s string) AdminKey {
	value := _AdminKey(s)
	return &value
}

type MailboxAddress interface {
	String() string
	IsEmpty() bool
	implementsOpaque()
}

type _MailboxAddress string

func (value *_MailboxAddress) implementsOpaque() {}

func (value *_MailboxAddress) String() string {
	return string(*value)
}

func (value *_MailboxAddress) IsEmpty() bool {
	return len(string(*value)) == 0
}
func NewMailboxAddress(s string) MailboxAddress {
	value := _MailboxAddress(s)
	return &value
}

type ReaderKey interface {
	String() string
	IsEmpty() bool
	implementsOpaque()
}

type _ReaderKey string

func (value *_ReaderKey) implementsOpaque() {}

func (value *_ReaderKey) String() string {
	return string(*value)
}

func (value *_ReaderKey) IsEmpty() bool {
	return len(string(*value)) == 0
}
func NewReaderKey(s string) ReaderKey {
	value := _ReaderKey(s)
	return &value
}

type IdempotentId interface {
	String() string
	IsEmpty() bool
	implementsOpaque()
}

type _IdempotentId string

func (value *_IdempotentId) implementsOpaque() {}

func (value *_IdempotentId) String() string {
	return string(*value)
}

func (value *_IdempotentId) IsEmpty() bool {
	return len(string(*value)) == 0
}
func NewIdempotentId(s string) IdempotentId {
	value := _IdempotentId(s)
	return &value
}

type CorrelationId interface {
	String() string
	IsEmpty() bool
	implementsOpaque()
}

type _CorrelationId string

func (value *_CorrelationId) implementsOpaque() {}

func (value *_CorrelationId) String() string {
	return string(*value)
}

func (value *_CorrelationId) IsEmpty() bool {
	return len(string(*value)) == 0
}
func NewCorrelationId(s string) CorrelationId {
	value := _CorrelationId(s)
	return &value
}

type ProcessUid interface {
	String() string
	IsEmpty() bool
	implementsOpaque()
}

type _ProcessUid string

func (value *_ProcessUid) implementsOpaque() {}

func (value *_ProcessUid) String() string {
	return string(*value)
}

func (value *_ProcessUid) IsEmpty() bool {
	return len(string(*value)) == 0
}
func NewProcessUid(s string) ProcessUid {
	value := _ProcessUid(s)
	return &value
}

type NatsSubject interface {
	String() string
	IsEmpty() bool
	implementsOpaque()
}

type _NatsSubject string

func (value *_NatsSubject) implementsOpaque() {}

func (value *_NatsSubject) String() string {
	return string(*value)
}

func (value *_NatsSubject) IsEmpty() bool {
	return len(string(*value)) == 0
}
func NewNatsSubject(s string) NatsSubject {
	value := _NatsSubject(s)
	return &value
}

type NatsStreamName interface {
	String() string
	IsEmpty() bool
	implementsOpaque()
}

type _NatsStreamName string

func (value *_NatsStreamName) implementsOpaque() {}

func (value *_NatsStreamName) String() string {
	return string(*value)
}

func (value *_NatsStreamName) IsEmpty() bool {
	return len(string(*value)) == 0
}
func NewNatsStreamName(s string) NatsStreamName {
	value := _NatsStreamName(s)
	return &value
}

type NatsConsumerName interface {
	String() string
	IsEmpty() bool
	implementsOpaque()
}

type _NatsConsumerName string

func (value *_NatsConsumerName) implementsOpaque() {}

func (value *_NatsConsumerName) String() string {
	return string(*value)
}

func (value *_NatsConsumerName) IsEmpty() bool {
	return len(string(*value)) == 0
}
func NewNatsConsumerName(s string) NatsConsumerName {
	value := _NatsConsumerName(s)
	return &value
}

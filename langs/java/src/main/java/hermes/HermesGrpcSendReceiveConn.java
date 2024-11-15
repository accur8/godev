package hermes;

import com.google.protobuf.ByteString;
import com.google.protobuf.InvalidProtocolBufferException;
import com.google.protobuf.MessageOrBuilder;
import com.google.protobuf.util.JsonFormat;
import io.grpc.ManagedChannel;
import io.grpc.stub.StreamObserver;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.Future;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;

public class HermesGrpcSendReceiveConn {

    final Logger logger = LoggerFactory.getLogger(getClass());

    final ManagedChannel grpcChannel;
    final CompletableFuture<Optional<Throwable>> complete;
    final InternalHermesClient internalHermesClient;
    final StreamObserver<Wsmessages.MessageFromClient> sendStream;

    long lastMessageReceived = System.currentTimeMillis();
    boolean active = true;

    final StreamObserver<Wsmessages.MessageToClient> receiveStream =
            new StreamObserver<Wsmessages.MessageToClient>() {
                @Override
                public void onNext(Wsmessages.MessageToClient messageToClient) {
                    lastMessageReceived = System.currentTimeMillis();
                    Wsmessages.MessageToClient.TestOneofCase toc = messageToClient.getTestOneofCase();
                    switch (toc) {
                        case MESSAGEENVELOPE:
                            receiveMessageEnvelope(messageToClient.getMessageEnvelope());
                            break;
                        case NOTIFICATION:
                            receiveNotification(messageToClient.getNotification());
                            break;
                        case PING:
                            receivePing(messageToClient.getPing());
                            break;
                        case PONG:
                            receivePong(messageToClient.getPong());
                            break;
                        case SENDMESSAGERESPONSE:
                            receiveSendMessageResponse(messageToClient.getSendMessageResponse());
                            break;
                        case TESTONEOF_NOT_SET:
                            logger.warn("received empty message");
                            break;
                    }
                }

                @Override
                public void onError(Throwable throwable) {
                    complete.complete(Optional.of(throwable));
                }

                @Override
                public void onCompleted() {
                    complete.complete(Optional.empty());
                }
            };

    public HermesGrpcSendReceiveConn(
            Wsmessages.CreateMailboxResponse mailbox,
            InternalHermesClient internalHermesClient
    ) {

        this.complete = new CompletableFuture<>();
        this.internalHermesClient = internalHermesClient;

        this.grpcChannel = internalHermesClient.config().newChannelBuilder().build();

        HermesServiceGrpc.HermesServiceStub hermesAsyncService =
                HermesServiceGrpc.newStub(this.grpcChannel);

        this.sendStream = hermesAsyncService.sendReceive(receiveStream);

        String startSeq;
        if (internalHermesClient.lastSequenceRead.get().isPresent()) {
            startSeq = internalHermesClient.lastSequenceRead.get().get().toString();
        } else {
            startSeq = "all";
        }

        List<Wsmessages.Subscription> subscriptions = new ArrayList<>();

        subscriptions.add(
            Wsmessages.Subscription
                .newBuilder()
                .setMailbox(
                    Wsmessages.MailboxSubscription
                        .newBuilder()
                        .setReaderKey(mailbox.getReaderKey())
                        .setChannel("rpc-inbox")
                        .setId("rpc-inbox")
                        .setStartSeq(startSeq)
                        .build()
                )
                .build()
        );

        Wsmessages.MessageFromClient firstMessage =
                Wsmessages.MessageFromClient
                        .newBuilder()
                        .setFirstMessage(
                                Wsmessages.FirstMessage
                                        .newBuilder()
                                        .setSenderInfo(
                                            Wsmessages.SenderInfo
                                                .newBuilder()
                                                .setAddress(mailbox.getAddress())
                                                .setReaderKey(mailbox.getReaderKey())
                                                .build()
                                        )
                                        .addAllSubscriptions(
                                                subscriptions
                                        )
                                        .setMailboxTimeoutInMs(5 * 60 * 1000)
                                        .build()
                        )
                        .build();

        sendMessageFromClient(firstMessage);

        this.complete.thenRun(() ->
                this.active = false
        );

        internalHermesClient.scheduledExecutorService().submit(() -> {
            resubmitNonAckedSendMessageRequests();
        });

        scheduleNextPing();

    }

    void resubmitNonAckedSendMessageRequests() {
        Iterable<Wsmessages.SendMessageRequest> messagesToResend = internalHermesClient.nonAckedSendMessageRequests();
        for (Wsmessages.SendMessageRequest sendMessageRequest : messagesToResend) {
            sendMessage(sendMessageRequest);
        }
    }

    boolean hasExpired() {
        long now = System.currentTimeMillis();
        if ( (lastMessageReceived + internalHermesClient.config().pingExpiryMilliSeconds()) < now ) {
            return true;
        } else {
            return false;
        }
    }

    void runExpiryCheck() {
        if ( hasExpired() ) {
            complete.complete(Optional.empty());
            internalHermesClient.reconnect();
        }
    }

    void scheduleNextPing() {

        if ( !active ) {
            return;
        }

        Runnable pingRunnable = () -> {
            if (active) {
                try {
                    Wsmessages.MessageFromClient ping =
                            Wsmessages.MessageFromClient
                                    .newBuilder()
                                    .setPing(
                                            Wsmessages.Ping
                                                    .newBuilder()
                                                    .setContext(ByteString.EMPTY)
                                                    .build()
                                    )
                                    .build();
                    sendMessageFromClient(ping);
                } catch (Exception e) {
                    logger.error("Error sending ping", e);
                }
                scheduleNextPing();
                runExpiryCheck();
            }
        };

        scheduledExecutorService().schedule(
                pingRunnable,
                this.internalHermesClient.config().pingIntervalSeconds,
                TimeUnit.SECONDS
        );

    }

    ScheduledExecutorService scheduledExecutorService() {
        return internalHermesClient.scheduledExecutorService();
    }


    void receiveMessageEnvelope(Wsmessages.MessageEnvelope messageEnvelope) {

        internalHermesClient.lastSequenceRead.set(Optional.of(messageEnvelope.getServerEnvelope().getSequence()));

        try {
            Wsmessages.Message message = Wsmessages.Message.parseFrom(messageEnvelope.getMessageBytes());

            logger.debug("received message " + ProtobufAssist.toJson(messageEnvelope));

            String correlationId = null;
            try {
                correlationId = message.getHeader().getRpcHeader().getCorrelationId();
            } catch (Exception e) {
            }

            CompletableFuture<Wsmessages.Message> mcf = internalHermesClient.correlations().get(correlationId);
            if ( mcf != null ) {
                internalHermesClient.correlations().remove(correlationId);
                mcf.complete(message);
            }

        } catch (Exception e) {
            logger.error("Error parsing message", e);
        }
    }

    void receivePing(Wsmessages.Ping ping) {
        Wsmessages.MessageFromClient pong =
            Wsmessages.MessageFromClient
                    .newBuilder()
                    .setPong(
                            Wsmessages.Pong
                                    .newBuilder()
                                    .setContext(ping.getContext())
                                    .build()
                    )
                    .build();
        sendMessageFromClient(pong);
    }

    void receiveSendMessageResponse(Wsmessages.SendMessageResponse sendMessageResponse) {
        logger.debug("received send message response " + ProtobufAssist.toJson(sendMessageResponse));
        internalHermesClient.ackSendMessageResponse(sendMessageResponse);
    }

    void receiveNotification(Wsmessages.Notification notification) {
        logger.info("Received notification: " + notification.getMessage());
    }

    void receivePong(Wsmessages.Pong pong) {
        logger.debug("received pong " + pong);
    }


    void sendMessageFromClient(Wsmessages.MessageFromClient messageFromClient) {
        if ( logger.isDebugEnabled() ) {
            logger.debug("sending " + ProtobufAssist.toJson(messageFromClient));
        }
        sendStream.onNext(messageFromClient);
    }

    void sendMessage(Wsmessages.SendMessageRequest sendMessageRequest) {
        sendMessageFromClient(
                Wsmessages.MessageFromClient
                        .newBuilder()
                        .setSendMessageRequest(sendMessageRequest)
                        .build()
        );
    }

    CompletableFuture<Wsmessages.Message> rpcCall(RpcRequest request) {

        String idempotentId = HermesClient.newIdempotentId();
        String correlationId = HermesClient.newCorrelationId();
        CompletableFuture<Wsmessages.Message> future = new CompletableFuture<>();

        internalHermesClient.correlations().put(correlationId, future);

        ByteString context;
        if (request.context != null) {
            context = ByteString.copyFrom(request.context);
        } else {
            context = ByteString.EMPTY;
        }

        ByteString requestBody;
        if (request.body != null) {
            requestBody = ByteString.copyFrom(request.body);
        } else {
            requestBody = ByteString.EMPTY;
        }

        List<Wsmessages.KeyValPair> extraHeaders = new ArrayList<>();
        if ( request.extraHeaders != null ) {
            for (String key : request.extraHeaders.keySet()) {
                extraHeaders.add(
                    Wsmessages.KeyValPair
                        .newBuilder()
                        .setKey(key)
                        .setVal(request.extraHeaders.get(key))
                        .build()
                );
            }
        }

        sendMessage(
            Wsmessages.SendMessageRequest
                .newBuilder()
                .addTo(request.to)
                .setChannel("rpc-inbox")
                .setMessage(
                    Wsmessages.Message
                        .newBuilder()
                        .setHeader(
                            Wsmessages.MessageHeader
                                .newBuilder()
                                .setRpcHeader(
                                    Wsmessages.RpcHeader
                                        .newBuilder()
                                        .setEndPoint(request.endPoint)
                                        .setCorrelationId(correlationId)
                                        .setContext(context)
                                        .setFrameType(Wsmessages.RpcFrameType.Request)
                                        .build()
                                )
                                .setContentType(request.contentType)
                                .setSender(internalHermesClient.mailbox().getAddress())
                                .addAllExtraHeaders(extraHeaders)
                                .build()
                        )
                        .setData(requestBody)
                        .setSenderEnvelope(
                            Wsmessages.SenderEnvelope
                                .newBuilder()
                                .setCreated(System.currentTimeMillis())
                                .build()
                        )
                        .build()
                )
                .build()
        );

        return future;
    }

    public void shutdown() {
        active = false;
        internalHermesClient.scheduledExecutorService.submit(() -> {
            try {
                grpcChannel.shutdown();
            } catch (Exception e) {
                logger.error("Error shutting down channel", e);
            }
        });
    }

}

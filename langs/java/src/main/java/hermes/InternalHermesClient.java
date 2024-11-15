package hermes;


import com.google.common.collect.ImmutableList;
import io.grpc.ManagedChannel;

import java.util.Map;
import java.util.Optional;
import java.util.concurrent.*;
import java.util.concurrent.atomic.AtomicReference;

public class InternalHermesClient extends HermesClient {

    final HermesClientConfig config;
    final Wsmessages.CreateMailboxResponse mailboxResponse;

    final ScheduledExecutorService scheduledExecutorService = Executors.newSingleThreadScheduledExecutor();
    final Map<String,CompletableFuture<Wsmessages.Message>> correlationsMap = new ConcurrentHashMap<>();
    final Map<String, Wsmessages.SendMessageRequest> waitingForAcks = new ConcurrentHashMap<>();

    final AtomicReference<Optional<Long>> lastSequenceRead = new AtomicReference(Optional.empty());

    final Object reconnectLock = new Object();

    HermesGrpcSendReceiveConn currentGrpcConnImpl = null;

    InternalHermesClient(HermesClientConfig config) {

        this.config = config;

        ManagedChannel tempInitialChannel = config.newChannelBuilder().build();
        HermesServiceGrpc.HermesServiceBlockingStub tempStub = HermesServiceGrpc.newBlockingStub(tempInitialChannel);

        this.mailboxResponse =
                tempStub.createMailbox(
                        Wsmessages
                                .CreateMailboxRequest
                                .newBuilder()
                                .addAllChannels(ImmutableList.of("rpc-sent", "rpc-inbox"))
                                .build()
                );

        this.reconnect();

    }

    public void reconnect() {
        if (currentGrpcConnImpl == null || !currentGrpcConnImpl.active) {
            synchronized (reconnectLock) {
                if (currentGrpcConnImpl == null || !currentGrpcConnImpl.active) {
                    if ( currentGrpcConnImpl != null ) {
                        currentGrpcConnImpl.shutdown();
                        currentGrpcConnImpl = null;
                    }

                    currentGrpcConnImpl =
                        new HermesGrpcSendReceiveConn(
                            mailboxResponse,
                            this
                        );

                }

            }
        }
    }

    @Override
    public Wsmessages.CreateMailboxResponse mailbox() {
        return mailboxResponse;
    }

    @Override
    public CompletableFuture<Wsmessages.Message> rpcCall(RpcRequest request) {
        reconnect();
        return currentGrpcConnImpl.rpcCall(request);
    }

    public void messageSent(Wsmessages.SendMessageRequest sendMessageRequest) {
        waitingForAcks.put(sendMessageRequest.getIdempotentId(), sendMessageRequest);
    }

    public void ackSendMessageResponse(Wsmessages.SendMessageResponse sendMessageResponse) {
        waitingForAcks.remove(sendMessageResponse.getIdempotentId());
    }

    public Iterable<Wsmessages.SendMessageRequest> nonAckedSendMessageRequests() {
        return waitingForAcks.values();
    }

    public Map<String, CompletableFuture<Wsmessages.Message>> correlations() {
        return correlationsMap;
    }

    public ScheduledExecutorService scheduledExecutorService() {
        return scheduledExecutorService;
    }

    public HermesClientConfig config() {
        return config;
    }

}

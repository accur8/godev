package hermes;

import java.util.concurrent.CompletableFuture;
import java.util.concurrent.Future;

public abstract class HermesClient {

    public static final String newIdempotentId() {
        return "ii" + java.util.UUID.randomUUID().toString().replace("-", "");
    };

    public static final String newCorrelationId() {
        return "cc" + java.util.UUID.randomUUID().toString().replace("-", "");
    };

    public static HermesClient build(HermesClientConfig config) {
        return new InternalHermesClient(config);
    }

    public abstract Wsmessages.CreateMailboxResponse mailbox();
    public abstract CompletableFuture<Wsmessages.Message> rpcCall(RpcRequest request);

}

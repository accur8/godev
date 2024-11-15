package hermes;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.net.URL;
import java.util.Collections;

public class TestClient {

    static Logger logger = LoggerFactory.getLogger(TestClient.class);

    public static void main(String[] args) {

        ch.qos.logback.classic.Logger l0 = (ch.qos.logback.classic.Logger) org.slf4j.LoggerFactory.getLogger("io.grpc.netty");
        l0.setLevel(ch.qos.logback.classic.Level.INFO);

        try {

            HermesClient client1 = newClient();
//            HermesClient client2 = newClient();

            byte[] emptyArray = new byte[0];

            RpcRequest req = new RpcRequest();
            req.body = emptyArray;
            req.extraHeaders = Collections.emptyMap();
            req.to = "bob";
//            req.to = client1.mailbox().getWriterKey();
            req.context = emptyArray;
            req.endPoint = "ping";
            req.contentType = Wsmessages.ContentType.Json;

            client1.rpcCall(req).thenApply( (msg) -> {
                logger.info("SUCCESS Got response: " + ProtobufAssist.toJson(msg));
                return null;
            });

            Thread.sleep(100000);

        } catch (Exception e) {
            e.printStackTrace();
        }


    }

    static HermesClient newClient() {
        try {
            HermesClientConfig config = new HermesClientConfig();
            //        config.rootUrl = new URL("https://hermes-grpc.ahsrcm.com");
            config.rootUrl = new URL("http://localhost:6081");
            config.pingIntervalSeconds = 10;
            HermesClient client = new InternalHermesClient(config);
            return client;
        } catch (Exception e) {
            e.printStackTrace();
            throw new RuntimeException(e);
        }
    }

}

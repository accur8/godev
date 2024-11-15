package hermes;

import io.grpc.*;

import java.net.URL;

public class HermesClientConfig {

    public URL rootUrl;
    public int pingIntervalSeconds;

    public int pingExpiryMilliSeconds() {
        return 1000 * (pingIntervalSeconds * 2);
    }

    public ManagedChannelBuilder<?> newChannelBuilder() {

        int port;
        if (rootUrl.getPort() == -1) {
            if (rootUrl.getProtocol().equals("http")) {
                port = 80;
            } else {
                port = 443;
            }
        } else {
            port = rootUrl.getPort();
        }

        String target = rootUrl.getHost() + ":" + port;

        boolean useSsl = rootUrl.getProtocol().equals("https");

        ManagedChannelBuilder<?> channelBuilder =
                ManagedChannelBuilder.forTarget(target);


        if ( useSsl ) {
//            channelCredentials = TlsChannelCredentials.newBuilder().build();
        } else {
            channelBuilder = channelBuilder.usePlaintext();
        }
        return channelBuilder;

    }

}

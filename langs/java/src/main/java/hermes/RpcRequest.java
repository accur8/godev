package hermes;

import java.util.Map;

public class RpcRequest {
    public String to;
    public String endPoint;
    public byte[] body;
    public Wsmessages.ContentType contentType;
    public Map<String,String> extraHeaders;
    public byte[] context;
}

package hermes;

import com.google.protobuf.InvalidProtocolBufferException;
import com.google.protobuf.MessageOrBuilder;
import com.google.protobuf.util.JsonFormat;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

public class ProtobufAssist {

    private static Logger logger = LoggerFactory.getLogger(ProtobufAssist.class);

    private static JsonFormat.Printer printer = JsonFormat.printer().omittingInsignificantWhitespace();

    public static String toJson(MessageOrBuilder mob) {
        String json = "";
        try {
            json = printer.print(mob);
        } catch (InvalidProtocolBufferException e ) {
            logger.error("swalling error printing message", e);
            json = mob.toString();
        }
        return json;
    }

}

package hermes;

import static io.grpc.MethodDescriptor.generateFullMethodName;

/**
 */
@javax.annotation.Generated(
    value = "by gRPC proto compiler (version 1.61.0)",
    comments = "Source: wsmessages.proto")
@io.grpc.stub.annotations.GrpcGenerated
public final class HermesServiceGrpc {

  private HermesServiceGrpc() {}

  public static final java.lang.String SERVICE_NAME = "hermes.HermesService";

  // Static method descriptors that strictly reflect the proto.
  private static volatile io.grpc.MethodDescriptor<hermes.Wsmessages.MessageFromClient,
      hermes.Wsmessages.MessageToClient> getSendReceiveMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "SendReceive",
      requestType = hermes.Wsmessages.MessageFromClient.class,
      responseType = hermes.Wsmessages.MessageToClient.class,
      methodType = io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
  public static io.grpc.MethodDescriptor<hermes.Wsmessages.MessageFromClient,
      hermes.Wsmessages.MessageToClient> getSendReceiveMethod() {
    io.grpc.MethodDescriptor<hermes.Wsmessages.MessageFromClient, hermes.Wsmessages.MessageToClient> getSendReceiveMethod;
    if ((getSendReceiveMethod = HermesServiceGrpc.getSendReceiveMethod) == null) {
      synchronized (HermesServiceGrpc.class) {
        if ((getSendReceiveMethod = HermesServiceGrpc.getSendReceiveMethod) == null) {
          HermesServiceGrpc.getSendReceiveMethod = getSendReceiveMethod =
              io.grpc.MethodDescriptor.<hermes.Wsmessages.MessageFromClient, hermes.Wsmessages.MessageToClient>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.BIDI_STREAMING)
              .setFullMethodName(generateFullMethodName(SERVICE_NAME, "SendReceive"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  hermes.Wsmessages.MessageFromClient.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  hermes.Wsmessages.MessageToClient.getDefaultInstance()))
              .setSchemaDescriptor(new HermesServiceMethodDescriptorSupplier("SendReceive"))
              .build();
        }
      }
    }
    return getSendReceiveMethod;
  }

  private static volatile io.grpc.MethodDescriptor<hermes.Wsmessages.CreateMailboxRequest,
      hermes.Wsmessages.CreateMailboxResponse> getCreateMailboxMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "CreateMailbox",
      requestType = hermes.Wsmessages.CreateMailboxRequest.class,
      responseType = hermes.Wsmessages.CreateMailboxResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<hermes.Wsmessages.CreateMailboxRequest,
      hermes.Wsmessages.CreateMailboxResponse> getCreateMailboxMethod() {
    io.grpc.MethodDescriptor<hermes.Wsmessages.CreateMailboxRequest, hermes.Wsmessages.CreateMailboxResponse> getCreateMailboxMethod;
    if ((getCreateMailboxMethod = HermesServiceGrpc.getCreateMailboxMethod) == null) {
      synchronized (HermesServiceGrpc.class) {
        if ((getCreateMailboxMethod = HermesServiceGrpc.getCreateMailboxMethod) == null) {
          HermesServiceGrpc.getCreateMailboxMethod = getCreateMailboxMethod =
              io.grpc.MethodDescriptor.<hermes.Wsmessages.CreateMailboxRequest, hermes.Wsmessages.CreateMailboxResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(SERVICE_NAME, "CreateMailbox"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  hermes.Wsmessages.CreateMailboxRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  hermes.Wsmessages.CreateMailboxResponse.getDefaultInstance()))
              .setSchemaDescriptor(new HermesServiceMethodDescriptorSupplier("CreateMailbox"))
              .build();
        }
      }
    }
    return getCreateMailboxMethod;
  }

  private static volatile io.grpc.MethodDescriptor<hermes.Wsmessages.AddChannelRequest,
      hermes.Wsmessages.AddChannelResponse> getAddChannelMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "AddChannel",
      requestType = hermes.Wsmessages.AddChannelRequest.class,
      responseType = hermes.Wsmessages.AddChannelResponse.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<hermes.Wsmessages.AddChannelRequest,
      hermes.Wsmessages.AddChannelResponse> getAddChannelMethod() {
    io.grpc.MethodDescriptor<hermes.Wsmessages.AddChannelRequest, hermes.Wsmessages.AddChannelResponse> getAddChannelMethod;
    if ((getAddChannelMethod = HermesServiceGrpc.getAddChannelMethod) == null) {
      synchronized (HermesServiceGrpc.class) {
        if ((getAddChannelMethod = HermesServiceGrpc.getAddChannelMethod) == null) {
          HermesServiceGrpc.getAddChannelMethod = getAddChannelMethod =
              io.grpc.MethodDescriptor.<hermes.Wsmessages.AddChannelRequest, hermes.Wsmessages.AddChannelResponse>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(SERVICE_NAME, "AddChannel"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  hermes.Wsmessages.AddChannelRequest.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.ProtoUtils.marshaller(
                  hermes.Wsmessages.AddChannelResponse.getDefaultInstance()))
              .setSchemaDescriptor(new HermesServiceMethodDescriptorSupplier("AddChannel"))
              .build();
        }
      }
    }
    return getAddChannelMethod;
  }

  /**
   * Creates a new async stub that supports all call types for the service
   */
  public static HermesServiceStub newStub(io.grpc.Channel channel) {
    io.grpc.stub.AbstractStub.StubFactory<HermesServiceStub> factory =
      new io.grpc.stub.AbstractStub.StubFactory<HermesServiceStub>() {
        @java.lang.Override
        public HermesServiceStub newStub(io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
          return new HermesServiceStub(channel, callOptions);
        }
      };
    return HermesServiceStub.newStub(factory, channel);
  }

  /**
   * Creates a new blocking-style stub that supports unary and streaming output calls on the service
   */
  public static HermesServiceBlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    io.grpc.stub.AbstractStub.StubFactory<HermesServiceBlockingStub> factory =
      new io.grpc.stub.AbstractStub.StubFactory<HermesServiceBlockingStub>() {
        @java.lang.Override
        public HermesServiceBlockingStub newStub(io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
          return new HermesServiceBlockingStub(channel, callOptions);
        }
      };
    return HermesServiceBlockingStub.newStub(factory, channel);
  }

  /**
   * Creates a new ListenableFuture-style stub that supports unary calls on the service
   */
  public static HermesServiceFutureStub newFutureStub(
      io.grpc.Channel channel) {
    io.grpc.stub.AbstractStub.StubFactory<HermesServiceFutureStub> factory =
      new io.grpc.stub.AbstractStub.StubFactory<HermesServiceFutureStub>() {
        @java.lang.Override
        public HermesServiceFutureStub newStub(io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
          return new HermesServiceFutureStub(channel, callOptions);
        }
      };
    return HermesServiceFutureStub.newStub(factory, channel);
  }

  /**
   */
  public interface AsyncService {

    /**
     */
    default io.grpc.stub.StreamObserver<hermes.Wsmessages.MessageFromClient> sendReceive(
        io.grpc.stub.StreamObserver<hermes.Wsmessages.MessageToClient> responseObserver) {
      return io.grpc.stub.ServerCalls.asyncUnimplementedStreamingCall(getSendReceiveMethod(), responseObserver);
    }

    /**
     */
    default void createMailbox(hermes.Wsmessages.CreateMailboxRequest request,
        io.grpc.stub.StreamObserver<hermes.Wsmessages.CreateMailboxResponse> responseObserver) {
      io.grpc.stub.ServerCalls.asyncUnimplementedUnaryCall(getCreateMailboxMethod(), responseObserver);
    }

    /**
     */
    default void addChannel(hermes.Wsmessages.AddChannelRequest request,
        io.grpc.stub.StreamObserver<hermes.Wsmessages.AddChannelResponse> responseObserver) {
      io.grpc.stub.ServerCalls.asyncUnimplementedUnaryCall(getAddChannelMethod(), responseObserver);
    }
  }

  /**
   * Base class for the server implementation of the service HermesService.
   */
  public static abstract class HermesServiceImplBase
      implements io.grpc.BindableService, AsyncService {

    @java.lang.Override public final io.grpc.ServerServiceDefinition bindService() {
      return HermesServiceGrpc.bindService(this);
    }
  }

  /**
   * A stub to allow clients to do asynchronous rpc calls to service HermesService.
   */
  public static final class HermesServiceStub
      extends io.grpc.stub.AbstractAsyncStub<HermesServiceStub> {
    private HermesServiceStub(
        io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected HermesServiceStub build(
        io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
      return new HermesServiceStub(channel, callOptions);
    }

    /**
     */
    public io.grpc.stub.StreamObserver<hermes.Wsmessages.MessageFromClient> sendReceive(
        io.grpc.stub.StreamObserver<hermes.Wsmessages.MessageToClient> responseObserver) {
      return io.grpc.stub.ClientCalls.asyncBidiStreamingCall(
          getChannel().newCall(getSendReceiveMethod(), getCallOptions()), responseObserver);
    }

    /**
     */
    public void createMailbox(hermes.Wsmessages.CreateMailboxRequest request,
        io.grpc.stub.StreamObserver<hermes.Wsmessages.CreateMailboxResponse> responseObserver) {
      io.grpc.stub.ClientCalls.asyncUnaryCall(
          getChannel().newCall(getCreateMailboxMethod(), getCallOptions()), request, responseObserver);
    }

    /**
     */
    public void addChannel(hermes.Wsmessages.AddChannelRequest request,
        io.grpc.stub.StreamObserver<hermes.Wsmessages.AddChannelResponse> responseObserver) {
      io.grpc.stub.ClientCalls.asyncUnaryCall(
          getChannel().newCall(getAddChannelMethod(), getCallOptions()), request, responseObserver);
    }
  }

  /**
   * A stub to allow clients to do synchronous rpc calls to service HermesService.
   */
  public static final class HermesServiceBlockingStub
      extends io.grpc.stub.AbstractBlockingStub<HermesServiceBlockingStub> {
    private HermesServiceBlockingStub(
        io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected HermesServiceBlockingStub build(
        io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
      return new HermesServiceBlockingStub(channel, callOptions);
    }

    /**
     */
    public hermes.Wsmessages.CreateMailboxResponse createMailbox(hermes.Wsmessages.CreateMailboxRequest request) {
      return io.grpc.stub.ClientCalls.blockingUnaryCall(
          getChannel(), getCreateMailboxMethod(), getCallOptions(), request);
    }

    /**
     */
    public hermes.Wsmessages.AddChannelResponse addChannel(hermes.Wsmessages.AddChannelRequest request) {
      return io.grpc.stub.ClientCalls.blockingUnaryCall(
          getChannel(), getAddChannelMethod(), getCallOptions(), request);
    }
  }

  /**
   * A stub to allow clients to do ListenableFuture-style rpc calls to service HermesService.
   */
  public static final class HermesServiceFutureStub
      extends io.grpc.stub.AbstractFutureStub<HermesServiceFutureStub> {
    private HermesServiceFutureStub(
        io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected HermesServiceFutureStub build(
        io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
      return new HermesServiceFutureStub(channel, callOptions);
    }

    /**
     */
    public com.google.common.util.concurrent.ListenableFuture<hermes.Wsmessages.CreateMailboxResponse> createMailbox(
        hermes.Wsmessages.CreateMailboxRequest request) {
      return io.grpc.stub.ClientCalls.futureUnaryCall(
          getChannel().newCall(getCreateMailboxMethod(), getCallOptions()), request);
    }

    /**
     */
    public com.google.common.util.concurrent.ListenableFuture<hermes.Wsmessages.AddChannelResponse> addChannel(
        hermes.Wsmessages.AddChannelRequest request) {
      return io.grpc.stub.ClientCalls.futureUnaryCall(
          getChannel().newCall(getAddChannelMethod(), getCallOptions()), request);
    }
  }

  private static final int METHODID_CREATE_MAILBOX = 0;
  private static final int METHODID_ADD_CHANNEL = 1;
  private static final int METHODID_SEND_RECEIVE = 2;

  private static final class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final AsyncService serviceImpl;
    private final int methodId;

    MethodHandlers(AsyncService serviceImpl, int methodId) {
      this.serviceImpl = serviceImpl;
      this.methodId = methodId;
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public void invoke(Req request, io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        case METHODID_CREATE_MAILBOX:
          serviceImpl.createMailbox((hermes.Wsmessages.CreateMailboxRequest) request,
              (io.grpc.stub.StreamObserver<hermes.Wsmessages.CreateMailboxResponse>) responseObserver);
          break;
        case METHODID_ADD_CHANNEL:
          serviceImpl.addChannel((hermes.Wsmessages.AddChannelRequest) request,
              (io.grpc.stub.StreamObserver<hermes.Wsmessages.AddChannelResponse>) responseObserver);
          break;
        default:
          throw new AssertionError();
      }
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public io.grpc.stub.StreamObserver<Req> invoke(
        io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        case METHODID_SEND_RECEIVE:
          return (io.grpc.stub.StreamObserver<Req>) serviceImpl.sendReceive(
              (io.grpc.stub.StreamObserver<hermes.Wsmessages.MessageToClient>) responseObserver);
        default:
          throw new AssertionError();
      }
    }
  }

  public static final io.grpc.ServerServiceDefinition bindService(AsyncService service) {
    return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
        .addMethod(
          getSendReceiveMethod(),
          io.grpc.stub.ServerCalls.asyncBidiStreamingCall(
            new MethodHandlers<
              hermes.Wsmessages.MessageFromClient,
              hermes.Wsmessages.MessageToClient>(
                service, METHODID_SEND_RECEIVE)))
        .addMethod(
          getCreateMailboxMethod(),
          io.grpc.stub.ServerCalls.asyncUnaryCall(
            new MethodHandlers<
              hermes.Wsmessages.CreateMailboxRequest,
              hermes.Wsmessages.CreateMailboxResponse>(
                service, METHODID_CREATE_MAILBOX)))
        .addMethod(
          getAddChannelMethod(),
          io.grpc.stub.ServerCalls.asyncUnaryCall(
            new MethodHandlers<
              hermes.Wsmessages.AddChannelRequest,
              hermes.Wsmessages.AddChannelResponse>(
                service, METHODID_ADD_CHANNEL)))
        .build();
  }

  private static abstract class HermesServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoFileDescriptorSupplier, io.grpc.protobuf.ProtoServiceDescriptorSupplier {
    HermesServiceBaseDescriptorSupplier() {}

    @java.lang.Override
    public com.google.protobuf.Descriptors.FileDescriptor getFileDescriptor() {
      return hermes.Wsmessages.getDescriptor();
    }

    @java.lang.Override
    public com.google.protobuf.Descriptors.ServiceDescriptor getServiceDescriptor() {
      return getFileDescriptor().findServiceByName("HermesService");
    }
  }

  private static final class HermesServiceFileDescriptorSupplier
      extends HermesServiceBaseDescriptorSupplier {
    HermesServiceFileDescriptorSupplier() {}
  }

  private static final class HermesServiceMethodDescriptorSupplier
      extends HermesServiceBaseDescriptorSupplier
      implements io.grpc.protobuf.ProtoMethodDescriptorSupplier {
    private final java.lang.String methodName;

    HermesServiceMethodDescriptorSupplier(java.lang.String methodName) {
      this.methodName = methodName;
    }

    @java.lang.Override
    public com.google.protobuf.Descriptors.MethodDescriptor getMethodDescriptor() {
      return getServiceDescriptor().findMethodByName(methodName);
    }
  }

  private static volatile io.grpc.ServiceDescriptor serviceDescriptor;

  public static io.grpc.ServiceDescriptor getServiceDescriptor() {
    io.grpc.ServiceDescriptor result = serviceDescriptor;
    if (result == null) {
      synchronized (HermesServiceGrpc.class) {
        result = serviceDescriptor;
        if (result == null) {
          serviceDescriptor = result = io.grpc.ServiceDescriptor.newBuilder(SERVICE_NAME)
              .setSchemaDescriptor(new HermesServiceFileDescriptorSupplier())
              .addMethod(getSendReceiveMethod())
              .addMethod(getCreateMailboxMethod())
              .addMethod(getAddChannelMethod())
              .build();
        }
      }
    }
    return result;
  }
}

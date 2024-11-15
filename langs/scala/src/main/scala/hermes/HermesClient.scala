package hermes

import a8.common.logging.LoggingF
import com.google.protobuf.ByteString
import hermes.HermesClient.{RpcRequest, WriterKey}
import hermes.TestClient.processStream
import hermes.wsmessages.{CreateMailboxRequest, CreateMailboxResponse, KeyValPair, Message, MessageFromClient, MessageHeader, MessageToClient, Pong, RpcHeader, SendMessageRequest, SendMessageResponse, ZioWsmessages}
import io.grpc.ManagedChannelBuilder
import scalapb.zio_grpc.ZManagedChannel
import zio.stream.ZStream
import zio.{Chunk, Layer, Scope, ZIO, ZIOAppArgs, ZLayer}

import scala.collection.immutable.Map
import a8.shared.SharedImports.*

import java.util.concurrent.atomic.AtomicLong

object HermesClient extends LoggingF {

  def randomCorrelationId(): CorrelationId = {
    java.util.UUID.randomUUID().toString.replace("-", "")
  }

  def randomIdempotentId(): IdempotentId = {
    java.util.UUID.randomUUID().toString.replace("-", "")
  }

  object StartSeq {
    inline def apply(s: String): StartSeq = s
  }
  opaque type StartSeq = String

  object WriterKey {
    inline def apply(s: String): WriterKey = s
  }
  opaque type WriterKey = String

  object IdempotentId {
    inline def apply(s: String): IdempotentId = s
  }
  opaque type IdempotentId = String

  object SubscriptionId {
    inline def apply(s: String): SubscriptionId = s
  }
  opaque type SubscriptionId = String

  case class ClientSubscription(
    id: SubscriptionId,
    protobufSubscriptionFn: StartSeq => hermes.wsmessages.Subscription,
  ) {

    lazy val lastSeq = new AtomicLong(0)

    def protoSubscription: hermes.wsmessages.Subscription = {
      val startSeq =
        if ( lastSeq.get() == 0 ) {
          StartSeq("all")
        } else {
          StartSeq(lastSeq.get().toString)
        }
      protobufSubscriptionFn(startSeq)
    }

  }

  object CorrelationId {
    inline def apply(s: String): CorrelationId = s
  }
  opaque type CorrelationId = String

  object ChannelName {
    inline def apply(s: String): ChannelName = s
  }
  opaque type ChannelName = String

  case class HermesClientConfig(
    serverAddress: String,
    serverPort: Int,
    serverUsePlainText: Boolean,
    rpcChannel: ChannelName = "rpc-inbox",
    extraChannels: Seq[ChannelName] = Seq("rpc-sent"),
    heartBeatEvery: zio.Duration = zio.Duration.fromScala(scala.concurrent.duration.Duration(2, scala.concurrent.duration.SECONDS)),
    purgeTimeoutInMillis: Option[Long] = None,
    closeTimeoutInMillis: Option[Long] = None,
  ) {
    def channels = rpcChannel +: extraChannels
    def connTimeoutMillis = (2.5 * heartBeatEvery.toMillis).toLong
  }

  val live: ZLayer[Scope & HermesClientConfig, Throwable, HermesClient] = ZLayer.fromZIO(effect)

  val effect: zio.ZIO[Scope & HermesClientConfig, Throwable, HermesClient] = {

    def managedChannel(config: HermesClientConfig): ZManagedChannel =
      ZManagedChannel(
        //        ManagedChannelBuilder.forAddress("ahs-hermes.accur8.net", 6081).usePlaintext(),
        ManagedChannelBuilder.forAddress(config.serverAddress, config.serverPort) match {
          case b if config.serverUsePlainText =>
            b.usePlaintext()
          case b =>
            b
        },
      )


    def createMailbox(config: HermesClientConfig): zio.Task[CreateMailboxResponse] = {
      val req =
        CreateMailboxRequest(
          channels = config.channels,
          purgeTimeoutInMillis = config.purgeTimeoutInMillis.getOrElse(0),
        )
      val effect =
        for {
          grpcClient <- ZioWsmessages.HermesServiceClient.scoped(managedChannel(config))
          mailbox <- grpcClient.createMailbox(req)
        } yield mailbox
      effect.scoped
    }

    zservice[HermesClientConfig]
      .flatMap { config =>
        for {
          sender <- zio.Queue.unbounded[MessageFromClient]
          mailbox <- createMailbox(config)
          _ <- loggerF.debug(s"created mailbox ${mailbox}")
          currentConRef <- zio.Ref.make(none[HermesConn])
          reconnectSema <- zio.Semaphore.make(1)
          client <-
            zsucceed(
              InternalHermesClient(
                config,
                currentConRef,
                reconnectSema,
                managedChannel(config),
                mailbox,
              )
            )
        } yield client

      }
  }
  case class RpcRequest(
    to: WriterKey,
    endPoint: String,
    body: Chunk[Byte],
    contentType: hermes.wsmessages.ContentType,
    headers: Map[String, String] = Map.empty,
    context: Chunk[Byte] = Chunk.empty,
  ) {
    lazy val extraHeaders =
      headers
        .map { case (k, v) =>
          KeyValPair(k, v)
        }
        .toSeq
  }

}


trait HermesClient {
  val mailbox: CreateMailboxResponse
  def rpcCall(request: RpcRequest): zio.Task[Message]
}

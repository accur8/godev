package hermes


import hermes.HermesClient.{ClientSubscription, CorrelationId, HermesClientConfig, IdempotentId, StartSeq, SubscriptionId, loggerF}
import hermes.wsmessages.{CreateMailboxRequest, CreateMailboxResponse, MailboxSubscription, Message, MessageFromClient, MessageToClient, SendMessageRequest, Subscription, ZioWsmessages}
import a8.shared.SharedImports.*
import scalapb.zio_grpc.ZManagedChannel
import zio.Task
import zio.stream.ZStream

class InternalHermesClient(
  val config: HermesClientConfig,
  currentConnRef: zio.Ref[Option[HermesConn]],
  reconnectSema: zio.Semaphore,
  zManagedChannel: ZManagedChannel,
  override val mailbox: CreateMailboxResponse,
) extends HermesClient with LoggingF {

  lazy val rpcSubscriptionId = SubscriptionId("rpc-inbox")

  def rpcSubscriptionFn(startSeq: StartSeq): Subscription =
    Subscription(
      testOneof = Subscription.TestOneof.Mailbox(MailboxSubscription(
        id = rpcSubscriptionId.toString,
        readerKey = mailbox.readerKey,
        channel = "rpc-inbox",
        startSeq = startSeq.toString,
      ))
    )

  lazy val rpcClientSubscription =
    ClientSubscription(
      SubscriptionId("rpc-inbox"),
      rpcSubscriptionFn,
    )

  lazy val correlations = scala.collection.concurrent.TrieMap[CorrelationId, zio.Promise[Throwable, Message]]()

  lazy val unAcknowledgedSentMessages = scala.collection.concurrent.TrieMap[IdempotentId, SendMessageRequest]()

  lazy val subscriptions = {
    val tempSubs = scala.collection.concurrent.TrieMap[SubscriptionId, ClientSubscription]()
    tempSubs.put(rpcClientSubscription.id, rpcClientSubscription)
    tempSubs
  }

  def updateLastSeq(subscriptionId: SubscriptionId, seqOpt: Option[Long]): Unit = {
    subscriptions.get(subscriptionId).foreach { clientSubscription =>
      seqOpt.filter(_ > 0).foreach { seq =>
//        logger.debug(s"setting last seq ${seq} ${subscriptionId}")
        clientSubscription.lastSeq.set(seq)
      }
    }
  }

  def ackSentMessage(idempotentId: IdempotentId): Unit = {
    unAcknowledgedSentMessages.remove(idempotentId)
  }

  def forkNewConn(prom: zio.Promise[Throwable, HermesConn]): zio.Task[Unit] = {
    val scopedEffect =
      for {
        sender <- zio.Queue.unbounded[MessageFromClient]
        grpcClient <- ZioWsmessages.HermesServiceClient.scoped(zManagedChannel)
        shutdownP <- zio.Promise.make[Throwable, Unit]
        hermesConn =
          HermesConn(
            internalClient = this,
            sender = sender,
            receiver = grpcClient.sendReceive(ZStream.fromQueue(sender)),
            shutdownP = shutdownP,
          )
        _ <- hermesConn.start
        _ <- prom.succeed(hermesConn)
        _ <- shutdownP.await
      } yield ()
    scopedEffect
      .scoped
      .forkDaemon
      .unit
  }

  def runReconnect: zio.Task[HermesConn] =
    reconnectSema.withPermit {
      for {
        connOpt <- currentConnRef.get
        conn <-
          connOpt match {
            case Some(conn) =>
              zsucceed(conn)
            case None =>
              for {
                prom <- zio.Promise.make[Throwable, HermesConn]
                _ <- forkNewConn(prom)
                conn <- prom.await
                _ <- currentConnRef.set(Some(conn))
              } yield conn
          }
      } yield conn
    }

  def currentConn: zio.Task[HermesConn] =
    currentConnRef.get.flatMap {
      case Some(conn) =>
        zsucceed(conn)
      case None =>
        runReconnect
    }

  def scheduleReconnect: zio.Task[Unit] =
    for {
      _ <- currentConnRef.set(None)
      _ <- runReconnect.forkDaemon
    } yield ()

  override def rpcCall(request: HermesClient.RpcRequest): Task[Message] =
    currentConn.flatMap(_.rpcCall(request))

}

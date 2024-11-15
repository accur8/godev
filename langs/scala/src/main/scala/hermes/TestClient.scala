package hermes

import a8.common.logging.Level
import a8.shared.app.BootstrappedIOApp
import a8.shared.app.BootstrappedIOApp.BootstrapEnv
import hermes.HermesClient.{HermesClientConfig, WriterKey}
import hermes.wsmessages.MessageFromClient.TestOneof
import hermes.wsmessages.{ContentType, CreateMailboxRequest, CreateMailboxResponse, FirstMessage, HermesServiceGrpc, MailboxSubscription, MessageFromClient, MessageToClient, SenderInfo, Subscription, WsmessagesProto, ZioWsmessages}
import io.grpc.ManagedChannelBuilder
import scalapb.zio_grpc.ZManagedChannel
import zio.stream.ZStream
import zio.{Chunk, Layer, Scope, ZIO, ZIOAppArgs, ZLayer}

object TestClient extends BootstrappedIOApp {

  val rpcServerMailbox = WriterKey("wwf929e461bd62462492f5f48041831549")

  val clientLayer: Layer[Throwable, ZioWsmessages.HermesServiceClient] =
    ZioWsmessages.HermesServiceClient.live(
      ZManagedChannel(
//        ManagedChannelBuilder.forAddress("ahs-hermes.accur8.net", 6081).usePlaintext(),
        ManagedChannelBuilder.forAddress("hermes-grpc.ahsrcm.com", 443),
      )
    )

  override def initialLogLevels: Iterable[(String, Level)] =
    super.initialLogLevels ++ Iterable(
      "io.grpc.netty" -> Level.Info,
    )

  override def runT: ZIO[BootstrapEnv, Throwable, Unit] =
    runX
//    runY

  def runX: ZIO[BootstrapEnv, Throwable, Unit] = {
    HermesClient
      .effect
      .flatMap(client =>
        def impl = {
          client.rpcCall(
            HermesClient.RpcRequest(
              to = rpcServerMailbox,
              endPoint = "ListSystemdServices",
              body = Chunk.empty,
              contentType = ContentType.Json,
            )
          )
          .flatMap( response =>
            loggerF.debug(s"received response - ${response}")
          )
          .delay(zio.Duration.fromSeconds(1))
        }
        impl *> impl *> impl *> impl *> impl *> impl *> impl *> impl *> impl *> impl
      )
      .provideSome[BootstrapEnv](ZLayer.succeed(
//        HermesClientConfig(
//          serverAddress = "hermes-grpc.ahsrcm.com",
//          serverPort = 443,
//          serverUsePlainText = false,
//          purgeTimeoutInMillis = Some(99L * 365 * 24 * 60 * 60 * 1000 /* aka 99 years*/),
//        )
//        HermesClientConfig(
//          serverAddress = "ahs-hermes.accur8.net",
//          serverPort = 6081,
//          serverUsePlainText = true,
//          purgeTimeoutInMillis = Some(99L * 365 * 24 * 60 * 60 * 1000 /* aka 99 years*/),
//        )
        HermesClientConfig(
          serverAddress = "localhost",
          serverPort = 6081,
          serverUsePlainText = true,
//          purgeTimeoutInMillis = Some(99L * 365 * 24 * 60 * 60 * 1000 /* aka 99 years*/),
        )
      ))


  }

//  def processClient(client: HermesClient): zio.Task[Unit] = {
//
//  }

  def runY: ZIO[BootstrapEnv, Throwable, Unit] = {

    val effect =
      for {
        sender <- zio.Queue.bounded[MessageFromClient](100)
        client <- zio.ZIO.service[ZioWsmessages.HermesServiceClient]
        mailbox <- client.createMailbox(CreateMailboxRequest(Seq("rpc-inbox", "rpc-sent")))
        _ <- processStream(mailbox, sender, client.sendReceive(ZStream.fromQueue(sender)))
      } yield ()

    effect.provide(clientLayer)

  }


  def sendFirstMessage(mailbox: CreateMailboxResponse, sender: zio.Queue[MessageFromClient], receiver: zio.stream.ZStream[Any, Throwable, MessageToClient]): ZIO[Any, Throwable, Unit] = {
    sender
      .offer(
        MessageFromClient(
          TestOneof.FirstMessage(
            FirstMessage(
              senderInfo = Some(SenderInfo(
                readerKey = mailbox.readerKey,
                address = mailbox.address,
              )),
              subscriptions = Seq(
                Subscription(Subscription.TestOneof.Mailbox(MailboxSubscription(
                  id = "rpc-inbox",
                  readerKey = mailbox.readerKey,
                  channel = "rpc-inbox",
                  startSeq = "all",
                ))),
              ),
            )
          )
        )
      )
      .as(())
  }

  def runReceiver(receiver: zio.stream.ZStream[Any,Throwable,MessageToClient], promiseRef: zio.Ref[Option[zio.Promise[Throwable, MessageToClient]]]): ZIO[Any, Throwable, Unit] = {
    receiver
      .mapZIO(message =>
        for {
          promise <- promiseRef.get
          _ <- promise match {
            case Some(p) =>
              p.succeed(message)
            case None =>
              zio.ZIO.unit
          }
        } yield ()
      )
      .runDrain
      .forkDaemon
      .as(())
  }

  def processStream(mailbox: CreateMailboxResponse, sender: zio.Queue[MessageFromClient], receiver: zio.stream.ZStream[Any,Throwable,MessageToClient]): ZIO[Any, Throwable, Unit] = {
    for {
      _ <- sendFirstMessage(mailbox, sender, receiver)
      promiseRef <- zio.Ref.make[Option[zio.Promise[Throwable, MessageToClient]]](None)
      _ <- runReceiver(receiver, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
      _ <- singlePing(sender, promiseRef)
    } yield ()
  }

  def now() = System.currentTimeMillis()
//  def now() = System.nanoTime()

  def singlePing(sender: zio.Queue[MessageFromClient], promiseRef: zio.Ref[Option[zio.Promise[Throwable, MessageToClient]]]): ZIO[Any, Throwable, Unit] = {
    val start = now()
    for {
      prom <- zio.Promise.make[Throwable, MessageToClient]
      _ <- promiseRef.set(Some(prom))
      _ <-
        sender.offer(
          MessageFromClient(
            TestOneof.Ping(
              wsmessages.Ping()
            )
          )
        )
      _ <- prom.await

    } yield {
      val delta = now() - start
      println(delta)
    }
  }

}

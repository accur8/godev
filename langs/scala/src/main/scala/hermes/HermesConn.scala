package hermes

import a8.common.logging.LoggingF
import a8.shared.SharedImports.*
import com.google.protobuf.ByteString
import hermes.HermesClient.*
import hermes.wsmessages.*
import zio.UIO

import java.util.concurrent.atomic.{AtomicBoolean, AtomicLong}

case class HermesConn(
  internalClient: InternalHermesClient,
  sender: zio.Enqueue[MessageFromClient],
  receiver: zio.stream.Stream[Throwable, MessageToClient],
  shutdownP: zio.Promise[Throwable, Unit],
)
  extends LoggingF
{

  def config = internalClient.config
  def mailbox = internalClient.mailbox

  lazy val lastPong = new AtomicLong(System.currentTimeMillis())

  def reconnectNeeded() = {
    val now = System.currentTimeMillis()
    val last = lastPong.get()
    val timeout = last + config.connTimeoutMillis
    now > timeout
  }

  def initiateReconnect: zio.Task[Unit] = {
    for {
      _ <- loggerF.debug("initiating reconnect")
      _ <- shutdownP.succeed(())
      _ <- internalClient.scheduleReconnect
    } yield ()
  }

  def forkPingerThread: zio.Task[Unit] = {

    def sendSinglePing = {
      loggerF.debug("sending ping") *>
      send(hermes.wsmessages.Ping())
    }

    def tailPings: zio.Task[Unit] = {
      for {
        _ <- zio.ZIO.sleep(config.heartBeatEvery)
        _ <- sendSinglePing
        shutdown <- shutdownP.isDone
        _ <-
          if ( shutdown ) {
            zunit
          } else if ( reconnectNeeded() ) {
            initiateReconnect
          } else {
            tailPings
          }
      } yield ()
    }

    tailPings
      .forkDaemon
      .unit

  }

  def start: zio.Task[Unit] = {
    for {
      _ <- forkReceiverThread
      _ <- sendFirstMessage
      _ <- sendUnAckedMessageRequests
      _ <- forkPingerThread
    } yield ()
  }

  def sendUnAckedMessageRequests: zio.Task[Unit] = {
    internalClient
      .unAcknowledgedSentMessages
      .values
      .map { req =>
        send(req)
      }
      .sequence
      .as(())
  }

  def send(msg: FirstMessage | SendMessageRequest | Ping | Pong) = {
    val testOneOf =
      msg match {
        case p: Pong =>
          MessageFromClient.TestOneof.Pong(p)
        case p: Ping =>
          MessageFromClient.TestOneof.Ping(p)
        case fm: FirstMessage =>
          MessageFromClient.TestOneof.FirstMessage(fm)
        case smr: SendMessageRequest =>
          MessageFromClient.TestOneof.SendMessageRequest(smr)
      }
    val mfc = MessageFromClient(testOneOf)
    for {
      _ <- loggerF.debug(s"sending message - ${mfc.toProtoString}")
      _ <- sender.offer(mfc)
    } yield ()
  }

  def sendFirstMessage: zio.Task[Unit] =
    send(
      FirstMessage(
        senderInfo = Some(SenderInfo(
          readerKey = mailbox.readerKey,
          address = mailbox.address,
        )),
        subscriptions = internalClient.subscriptions.values.map(_.protoSubscription).toSeq,
      )
    )

  def resolveCorrelatedPromise(correlationId: CorrelationId, message: Message): zio.Task[Unit] = {

    internalClient.correlations.get(correlationId) match {
      case Some(promise) =>
        promise
          .succeed(message)
          .tap(_ => zblock(internalClient.correlations.remove(correlationId)))
          .unit
      case None =>
        loggerF.debug(s"no correlation found for message - ${message}")
    }

  }

  def forkReceiverThread: zio.Task[Unit] =
    receiver
      .haltWhen(shutdownP)
      .mapZIO { message =>
        message.testOneof match {
          case MessageToClient.TestOneof.MessageEnvelope(envelope) =>
            val message = Message.parseFrom(envelope.messageBytes.toByteArray)
            envelope.serverEnvelope.map(_.subscriptionId).foreach { subscriptionIdStr =>
              val subscriptionId = SubscriptionId(subscriptionIdStr)
              internalClient.updateLastSeq(subscriptionId, envelope.serverEnvelope.map(_.sequence))
            }
            val correlationIdOpt =
              message
                .header
                .flatMap(_.rpcHeader)
                .map(_.correlationId)
                .filter(_.nonEmpty)
                .map(CorrelationId(_))
            correlationIdOpt match {
              case Some(correlationId) =>
                resolveCorrelatedPromise(correlationId, message)
              case None =>
                zunit
            }

          case MessageToClient.TestOneof.SubscribeResponse(response) =>
            loggerF.debug(s"received subscribe response - ${response.toProtoString}")

          case MessageToClient.TestOneof.Ping(ping) =>
            send(Pong(ping.context))
          case MessageToClient.TestOneof.Pong(pong) =>
            lastPong.set(System.currentTimeMillis())
            loggerF.debug(s"Received pong - ${pong.context}")

          case MessageToClient.TestOneof.SendMessageResponse(response) =>
            internalClient.ackSentMessage(IdempotentId(response.idempotentId))
            loggerF.debug(s"Received send message response - ${response}")

          case MessageToClient.TestOneof.Notification(notification) =>
            loggerF.info(s"Received notification - ${notification.message}")

          case MessageToClient.TestOneof.Empty =>
            loggerF.debug(s"Received empty message - ${message}")

        }
      }
      .either
      .mapZIO {
        case Left(e: io.grpc.StatusException) =>
          loggerF.debug(s"initiating reconnect, error in receiver thread", e) *>
            initiateReconnect
        case _ =>
          zunit
      }
      .runDrain
      .forkDaemon
      .as(())

  def send(correlationId: CorrelationId, req: RpcRequest): zio.Task[Unit] = {
    sendSendMessageRequest(
      SendMessageRequest(
        idempotentId = HermesClient.randomIdempotentId().toString,
        to = Seq(req.to.toString),
        channel = config.rpcChannel.toString,
        message = Some(Message(
          header = Some(MessageHeader(
            sender = mailbox.address,
            contentType = req.contentType,
            extraHeaders = req.extraHeaders,
            rpcHeader = Some(RpcHeader(
              context = ByteString.copyFrom(req.context.toArray),
              correlationId = correlationId.toString,
              endPoint = req.endPoint,
              frameType = wsmessages.RpcFrameType.Request,
            )),
          )),
          senderEnvelope = Some(wsmessages.SenderEnvelope(
            created = System.currentTimeMillis(),
          )),
          data = ByteString.copyFrom(req.body.toArray)
        )),
      )
    )
  }

  def sendSendMessageRequest(sendMessageRequest: SendMessageRequest): UIO[Unit] = {
    internalClient.unAcknowledgedSentMessages.put(IdempotentId(sendMessageRequest.idempotentId), sendMessageRequest)
    val mfc =
      MessageFromClient(
        MessageFromClient.TestOneof.SendMessageRequest(
          sendMessageRequest
        )
      )
    for {
      _ <- loggerF.debug(s"sending message - ${mfc}")
      _ <- sender.offer(mfc)
    } yield ()
  }

  def rpcCall(req: RpcRequest): zio.Task[Message] =
    for {
      correlationId <- zio.ZIO.succeed(randomCorrelationId())
      promise <- zio.Promise.make[Throwable, Message]
      _ <- zblock(internalClient.correlations.put(correlationId, promise))
      _ <- send(correlationId, req)
      responseMessage <- promise.await
    } yield responseMessage

}

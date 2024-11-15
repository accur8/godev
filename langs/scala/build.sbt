Compile / PB.targets := Seq(
 scalapb.gen() -> (Compile / sourceManaged).value / "scalapb",
 scalapb.zio_grpc.ZioCodeGenerator -> (Compile / sourceManaged).value
)

scalaVersion := "3.3.1"

// (optional) If you need scalapb/scalapb.proto or anything from
// google/protobuf/*.proto
libraryDependencies ++= Seq(
    // "io.grpc" % "grpc-netty" % scalapb.compiler.Version.grpcJavaVersion,
    "com.thesamet.scalapb" %% "scalapb-runtime" % scalapb.compiler.Version.scalapbVersion,
    "com.thesamet.scalapb" %% "scalapb-runtime-grpc" % scalapb.compiler.Version.scalapbVersion,
    "com.thesamet.scalapb.zio-grpc" %% "zio-grpc-core" % "0.6.0-rc6",
    "io.grpc" % "grpc-netty" % "1.61.1",
    "io.accur8" %% "a8-sync-api" % "1.0.0-20231222_1512_master",
    "com.google.protobuf" % "protobuf-java" % "3.13.0" % "protobuf",
)
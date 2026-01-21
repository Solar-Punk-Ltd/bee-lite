# bee-lite

[![Go Reference](https://pkg.go.dev/badge/github.com/Solar-Punk-Ltd/bee-lite.svg)](https://pkg.go.dev/github.com/Solar-Punk-Ltd/bee-lite)

bee-lite is an embeddable, lightweight bee node for applications to use swarm directly

## How to run

```go
lo := &beelite.LiteOptions {
    FullNodeMode:             false,
    BootnodeMode:             false,
    Bootnodes:                []string{"/dnsaddr/mainnet.ethswarm.org"},
    StaticNodes:              []string{<STATIC_NODE_ADDRESSES>},
    DataDir:                  dataDir,
    WelcomeMessage:           "Welcome from bee-lite by Solar Punk",
    BlockchainRpcEndpoint:    <RPC_ENDPOINT>,
    SwapInitialDeposit:       "10000000000000000",
    PaymentThreshold:         "100000000",
    SwapEnable:               true,
    ChequebookEnable:         true,
    DebugAPIEnable:           false,
    UsePostageSnapshot:       false,
    Mainnet:                  true,
    NetworkID:                1,
    NATAddr:                  "<NAT_ADDRESS>:<PORT>",
    CacheCapacity:            32 * 1024 * 1024,
    DBOpenFilesLimit:         50,
    DBWriteBufferSize:        32 * 1024 * 1024,
    DBBlockCacheCapacity:     32 * 1024 * 1024,
    DBDisableSeeksCompaction: false,
    RetrievalCaching:         true,
}

const loglevel = "4"
bl, err := beelite.Start(lo, password, loglevel)
if err != nil {
    return err
}
```

## Development for mobile using [gomobile](https://pkg.go.dev/golang.org/x/mobile/cmd/gomobile)

### Requirements

Go version 1.24.2
Gomobile require JDK 1.8 or later - I recommend JDK 17 or later

### Installation

1. Install golang. Recommended to use [goenv](https://github.com/go-nv/goenv) on Mac.
2. Install JDK - version 17 will do it for not. Recommend to use some tool - I have used [SDKMAN](https://sdkman.io/) for it.
3. Install Android Studio for easy SDK/NDK management. Android 16 - Api 36 installed with NDK 29.0.14206865
4. Here are some of my configured env vars maybe it helps you

   ```bash
   GOROOT=/Users/YOUR_USERNAME/.goenv/versions/1.24.2
   GOPATH=/Users/YOUR_USERNAME/go/1.24.2
   ANDROID_HOME=/Users/YOUR_USERNAME/Library/Android/sdk
   ANDROID_NDK_HOME=/Users/YOUR_USERNAME/Library/Android/sdk/ndk/29.0.14206865
   SDKMAN_DIR=/Users/YOUR_USERNAME/.sdkman
   SDKMAN_CANDIDATES_API=https://api.sdkman.io/2
   SDKMAN_BROKER_API=https://broker.sdkman.io
   SDKMAN_PLATFORM=darwinarm64
   SDKMAN_CANDIDATES_DIR=/Users/YOUR_USERNAME/.sdkman/candidates
   binary_input=/Users/YOUR_USERNAME/.sdkman/tmp/java-17.0.12-tem.bin
   ```

## Development for Android platform

Android has networking restrictions since API 30+ so to make [libp2p work](https://github.com/libp2p/go-libp2p/issues/1956) tweaks required on the Go repository that will be used to compile the code.
Follow the steps to solve this known issues.

Based on the following github issues:
[dnsconfig_unix.go](https://github.com/golang/go/issues/8877)
[netlink_linux.go, interface_linux.go](https://github.com/golang/go/issues/40569)

1. Copy the **\_android** files under the **net/** and **syscall/** subfolders of this repo to their respective folders under your go installation, e.g.:

   ```bash
   cp ./patch/net/* /Users/username/.goenv/versions/1.24.2/src/net/
   cp ./patch/syscall/* /Users/username/.goenv/versions/1.24.2/src/syscall/
   ```

2. Furthermore, open and add the following build directives to the existing dnsconfig_unix, interface_linux, netlink_linux files:

   ```go
   //go:build !android
   ```

   or if a windows exclusion exists add android like this

   ```go
   //go:build !windows && !android
   ```

   So when compiling go code to android target the newly added sources will run and those have fixes for the issues above.

3. To make these changes effect you should recompile the go binaries from the modified sources above - with the newly added \*\_android files .

   For this go to source folder root /Users/username/.goenv/versions/1.24.2/src/ for example.
   Search for make.bash and open it.
   Search for 'bootgo' in this file.
   That version is required to compile the target Go version.
   Install it and point GOROOT_BOOTSTRAP to that GOROOT like GOROOT_BOOTSTRAP=/Users/username/.goenv/versions/1.22.6 on Mac.
   After this run make.bash and it should compile the distro

## Compile with gomobile for Android

Target android api 21 is a nice sweet spot because it is Android Version: 5.0 (Lollipop) which is widely supported.

First you need gomobile and bind

```bash
go install golang.org/x/mobile/cmd/gomobile@latest
go get golang.org/x/mobile/bind
```

In the root of this project run
`gomobile init`

Run the following:

`gomobile bind -target=android -androidapi=21 -o bee-lite.aar`

## Important notes

- gomobile has serious type limitation thats why complex types cannot be exported to the Java / JNI bridge easily. For details check [these](https://github.com/Solar-Punk-Ltd/swarm-mobile-android) type restrictions section.
- gomobile will `export` the public fields of the directory where it is executed! Of course the other and referenced code will work in the binary but those sources wont be visible in the .jar as .Java classes

## Other references

The output can be used in [Android](https://dev.to/nikl/using-golang-gomobile-to-build-android-application-with-code-18jo) or in [React Native code (with Bridging)](https://medium.com/@ykanavalik/how-to-run-golang-code-in-your-react-native-android-application-using-expo-go-d4e46438b753)

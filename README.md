# bee-lite

[![Go Reference](https://pkg.go.dev/badge/github.com/Solar-Punk-Ltd/bee-lite.svg)](https://pkg.go.dev/github.com/Solar-Punk-Ltd/bee-lite)

bee-lite is an embeddable, lightweight bee node for applications to use swarm directly

## How to run

```
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

## Compile with gomobile for android

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

The output can be used in [Android](https://dev.to/nikl/using-golang-gomobile-to-build-android-application-with-code-18jo) or in [React Native code (with Bridging)](https://medium.com/@ykanavalik/how-to-run-golang-code-in-your-react-native-android-application-using-expo-go-d4e46438b753)

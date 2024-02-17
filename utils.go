package beelite

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethersphere/bee/pkg/api"
	chaincfg "github.com/ethersphere/bee/pkg/config"
	"github.com/ethersphere/bee/pkg/crypto"
	"github.com/ethersphere/bee/pkg/feeds"
	beelog "github.com/ethersphere/bee/pkg/log"
	"github.com/ethersphere/bee/pkg/postage"
	"github.com/ethersphere/bee/pkg/postage/postagecontract"
	"github.com/ethersphere/bee/pkg/settlement/swap/chequebook"
	"github.com/ethersphere/bee/pkg/storage"
	storer "github.com/ethersphere/bee/pkg/storer"
	"github.com/ethersphere/bee/pkg/swarm"
	"github.com/ethersphere/bee/pkg/topology"
)

const (
	LoggerName                  = "beelite"
	feedMetadataEntryOwner      = "swarm-feed-owner"
	feedMetadataEntryTopic      = "swarm-feed-topic"
	feedMetadataEntryType       = "swarm-feed-type"
	balanceCheckBackoffDuration = 20 * time.Second
	erc20SmallUnitStr           = "10000000000000000"
	ethSmallUnitStr             = "1000000000000000000"
	overlayNonce                = "overlayV2_nonce"
	noncedOverlayKey            = "nonce-overlay"
)

type Storer interface {
	storer.UploadStore
	storer.PinStore
	storer.CacheStore
	storer.NetStore
	storer.LocalStore
	storer.RadiusChecker
	storer.Debugger
}

type Beelite struct {
	Bee               *Bee
	OverlayEthAddress common.Address
	FeedFactory       feeds.Factory
	Storer            Storer
	Logger            beelog.Logger
	TopologyDriver    topology.Driver
	Ctx               context.Context
	ChequebookSvc     chequebook.Service
	Post              postage.Service
	Signer            crypto.Signer
	PostageContract   postagecontract.Interface
	StamperStore      storage.Store
	BatchStore        postage.Storer
	BeeNodeMode       api.BeeNodeMode
}

type putterOptions struct {
	BatchID  []byte
	TagID    uint64
	Deferred bool
	Pin      bool
}

type putterSessionWrapper struct {
	storer.PutterSession
	stamper postage.Stamper
	save    func() error
}

// noOpChequebookService is a noOp implementation for chequebook.Service interface.
type noOpChequebookService struct{}

func (m *noOpChequebookService) Deposit(context.Context, *big.Int) (hash common.Hash, err error) {
	return hash, postagecontract.ErrChainDisabled
}
func (m *noOpChequebookService) Withdraw(context.Context, *big.Int) (hash common.Hash, err error) {
	return hash, postagecontract.ErrChainDisabled
}
func (m *noOpChequebookService) WaitForDeposit(context.Context, common.Hash) error {
	return postagecontract.ErrChainDisabled
}
func (m *noOpChequebookService) Balance(context.Context) (*big.Int, error) {
	return nil, postagecontract.ErrChainDisabled
}
func (m *noOpChequebookService) AvailableBalance(context.Context) (*big.Int, error) {
	return nil, postagecontract.ErrChainDisabled
}
func (m *noOpChequebookService) Address() common.Address {
	return common.Address{}
}
func (m *noOpChequebookService) Issue(context.Context, common.Address, *big.Int, chequebook.SendChequeFunc) (*big.Int, error) {
	return nil, postagecontract.ErrChainDisabled
}
func (m *noOpChequebookService) LastCheque(common.Address) (*chequebook.SignedCheque, error) {
	return nil, postagecontract.ErrChainDisabled
}
func (m *noOpChequebookService) LastCheques() (map[common.Address]*chequebook.SignedCheque, error) {
	return nil, postagecontract.ErrChainDisabled
}

func newLogger(loggerName string, verbosity string) (beelog.Logger, error) {
	var (
		vLevel beelog.Level
	)

	switch verbosity {
	case "0", "silent":
		vLevel = beelog.VerbosityInfo
	case "1", "error":
		vLevel = beelog.VerbosityError
	case "2", "warn":
		vLevel = beelog.VerbosityWarning
	case "3", "info":
		vLevel = beelog.VerbosityInfo
	case "4", "debug":
		vLevel = beelog.VerbosityDebug
	case "5", "trace":
		vLevel = beelog.VerbosityDebug + 1
	default:
		return nil, fmt.Errorf("unknown verbosity level %q", verbosity)
	}

	beelog.ModifyDefaults(
		beelog.WithTimestamp(),
		beelog.WithLogMetrics(),
	)

	return beelog.NewLogger(
		loggerName,
		beelog.WithVerbosity(vLevel),
	).Register(), nil
}

func getConfigByNetworkID(networkID uint64) *networkConfig {
	config := networkConfig{
		blockTime: 5 * time.Second,
	}
	switch networkID {
	case chaincfg.Mainnet.NetworkID:
		config.bootNodes = []string{"/dnsaddr/mainnet.ethswarm.org"}
		config.blockTime = 5 * time.Second
		config.chainID = chaincfg.Mainnet.ChainID
	case 5: // Staging.
		config.chainID = chaincfg.Testnet.ChainID
	case chaincfg.Testnet.NetworkID:
		config.bootNodes = []string{"/dnsaddr/testnet.ethswarm.org"}
		config.blockTime = 15 * time.Second
		config.chainID = chaincfg.Testnet.ChainID
	default: // Will use the value provided by the chain.
		config.chainID = -1
	}

	return &config
}

var (
	errBatchUnusable               = errors.New("batch not usable")
	errUnsupportedDevNodeOperation = errors.New("operation not supported in dev mode")
)

func (bl *Beelite) getStamper(batchID []byte) (postage.Stamper, func() error, error) {
	exists, err := bl.BatchStore.Exists(batchID)
	if err != nil {
		return nil, nil, fmt.Errorf("batch exists: %w", err)
	}

	issuer, save, err := bl.Post.GetStampIssuer(batchID)
	if err != nil {
		return nil, nil, fmt.Errorf("stamp issuer: %w", err)
	}

	if usable := exists && bl.Post.IssuerUsable(issuer); !usable {
		return nil, nil, errBatchUnusable
	}

	return postage.NewStamper(bl.StamperStore, issuer, bl.Signer), save, nil
}

func (bl *Beelite) newStamperPutter(ctx context.Context, opts putterOptions) (storer.PutterSession, error) {
	if !opts.Deferred && bl.BeeNodeMode == api.DevMode {
		return nil, errUnsupportedDevNodeOperation
	}

	stamper, save, err := bl.getStamper(opts.BatchID)
	if err != nil {
		return nil, fmt.Errorf("get stamper: %w", err)
	}

	var session storer.PutterSession
	if opts.Deferred || opts.Pin {
		session, err = bl.Storer.Upload(ctx, opts.Pin, opts.TagID)
	} else {
		session = bl.Storer.DirectUpload()
	}

	if err != nil {
		return nil, fmt.Errorf("failed creating session: %w", err)
	}

	return &putterSessionWrapper{
		PutterSession: session,
		stamper:       stamper,
		save:          save,
	}, nil
}

// getOrCreateSessionID attempts to get the session if an tag id is supplied, and returns an error
// if it does not exist. If no id is supplied, it will attempt to create a new session and return it.
func (bl *Beelite) getOrCreateSessionID(tagUid uint64) (uint64, error) {
	var (
		tag storer.SessionInfo
		err error
	)
	// if tag ID is not supplied, create a new tag
	if tagUid == 0 {
		tag, err = bl.Storer.NewSession()
	} else {
		tag, err = bl.Storer.Session(tagUid)
	}
	return tag.TagID, err
}

// checkOverlay checks the overlay is the same as stored in the statestore
func checkOverlay(storer storage.StateStorer, overlay swarm.Address) error {

	var storedOverlay swarm.Address
	err := storer.Get(noncedOverlayKey, &storedOverlay)
	if err != nil {
		if !errors.Is(err, storage.ErrNotFound) {
			return err
		}
		return storer.Put(noncedOverlayKey, overlay)
	}

	if !storedOverlay.Equal(overlay) {
		return fmt.Errorf("overlay address changed. was %s before but now is %s", storedOverlay, overlay)
	}

	return nil
}

func overlayNonceExists(s storage.StateStorer) ([]byte, bool, error) {
	nonce := make([]byte, 32)
	if err := s.Get(overlayNonce, &nonce); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nonce, false, nil
		}
		return nil, false, err
	}
	return nonce, true, nil
}

func setOverlay(s storage.StateStorer, overlay swarm.Address, nonce []byte) error {
	return errors.Join(
		s.Put(overlayNonce, nonce),
		s.Put(noncedOverlayKey, overlay),
	)
}

func (bl *Beelite) ChequebookAddr() common.Address {
	if bl.ChequebookSvc != nil {
		return bl.ChequebookSvc.Address()
	}
	return common.HexToAddress(swarm.ZeroAddress.String())
}

func (bl *Beelite) ChequebookBalance() (*big.Int, error) {
	if bl.ChequebookSvc != nil {
		return bl.ChequebookSvc.Balance(bl.Ctx)
	}
	return nil, fmt.Errorf("chequebook not initialised")
}

func (bl *Beelite) ChequebookWithdraw(amount *big.Int) (common.Hash, error) {
	if bl.ChequebookSvc != nil {
		return bl.ChequebookSvc.Withdraw(bl.Ctx, amount)
	}
	return common.HexToHash(""), fmt.Errorf("chequebook not initialised")
}

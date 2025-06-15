package beelite

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethersphere/bee/v2/pkg/feeds"
	"github.com/ethersphere/bee/v2/pkg/file/loadsave"
	"github.com/ethersphere/bee/v2/pkg/file/pipeline"
	"github.com/ethersphere/bee/v2/pkg/file/pipeline/builder"
	"github.com/ethersphere/bee/v2/pkg/file/redundancy"
	"github.com/ethersphere/bee/v2/pkg/manifest"
	"github.com/ethersphere/bee/v2/pkg/storage"
	"github.com/ethersphere/bee/v2/pkg/swarm"
)

type pipelineFunc func(context.Context, io.Reader) (swarm.Address, error)

func requestPipelineFn(s storage.Putter, encrypt bool, rLevel redundancy.Level) pipelineFunc {
	return func(ctx context.Context, r io.Reader) (swarm.Address, error) {
		pipe := builder.NewPipelineBuilder(ctx, s, encrypt, rLevel)
		return builder.FeedPipeline(ctx, pipe, r)
	}
}

func requestPipelineFactory(ctx context.Context, s storage.Putter, encrypt bool, rLevel redundancy.Level) func() pipeline.Interface {
	return func() pipeline.Interface {
		return builder.NewPipelineBuilder(ctx, s, encrypt, rLevel)
	}
}

func (bl *Beelite) AddFeed(ctx context.Context,
	batchHex,
	owner,
	topic string,
	act bool,
	historyAddress swarm.Address,
	encrypt bool,
	rLevel redundancy.Level,
) (reference swarm.Address, newHistoryAddress swarm.Address, err error) {
	reference = swarm.ZeroAddress
	ownerB, err := hex.DecodeString(owner)
	if err != nil {
		bl.logger.Debug("feed put: decode owner: %v", err)
		return
	}
	topicB, err := hex.DecodeString(topic)
	if err != nil {
		bl.logger.Debug("feed put: decode topic: %v", err)
		return
	}
	if batchHex == "" {
		err = fmt.Errorf("batch is not set")
		return
	}
	batch, err := hex.DecodeString(batchHex)
	if err != nil {
		err = errInvalidPostageBatch
		return
	}
	var (
		tag      uint64
		deferred = false
		pin      = false
	)

	if deferred || pin {
		tag, err = bl.getOrCreateSessionID(uint64(0))
		if err != nil {
			bl.logger.Error(err, "get or create tag failed")
			return
		}
	}
	putter, err := bl.newStamperPutter(ctx, putterOptions{
		BatchID:  batch,
		TagID:    tag,
		Pin:      pin,
		Deferred: deferred,
	})
	if err != nil {
		bl.logger.Error(err, "get putter failed")
		return
	}

	factory := requestPipelineFactory(ctx, putter, encrypt, rLevel)
	l := loadsave.New(bl.storer.ChunkStore(), bl.storer.Cache(), factory, redundancy.DefaultLevel)
	feedManifest, err := manifest.NewDefaultManifest(l, encrypt)
	if err != nil {
		bl.logger.Debug("feed put: create manifest failed: %v", err)
		return
	}

	meta := map[string]string{
		feedMetadataEntryOwner: hex.EncodeToString(ownerB),
		feedMetadataEntryTopic: hex.EncodeToString(topicB),
		feedMetadataEntryType:  feeds.Sequence.String(), // only sequence allowed for now
	}

	emptyAddr := make([]byte, 32)
	// a feed manifest stores the metadata at the root "/" path
	err = feedManifest.Add(ctx, "/", manifest.NewEntry(swarm.NewAddress(emptyAddr), meta))
	if err != nil {
		bl.logger.Debug("feed post: add manifest entry failed: %v", err)
		return
	}
	reference, err = feedManifest.Store(ctx)
	if err != nil {
		bl.logger.Debug("feed post: store manifest failed: %v", err)
		return
	}

	encryptedReference := reference
	if act {
		reference, newHistoryAddress, err = bl.actEncryptionHandler(ctx, putter, reference, historyAddress)
		if err != nil {
			bl.logger.Error(err, "access control upload failed")
			return
		}
	}

	err = putter.Done(reference)
	if err != nil {
		bl.logger.Error(err, "done split failed")
		err = errors.Join(fmt.Errorf("done split failed: %w", err), putter.Cleanup())
		return
	}

	reference = encryptedReference
	return
}

func (bl *Beelite) FeedGetHandler(ctx context.Context, owner common.Address, topic []byte, at int64, after uint64) (swarm.Address, error) {
	logger := bl.logger.WithName("get_feed").Build()

	if at == 0 {
		at = time.Now().Unix()
	}

	f := feeds.New(topic, owner)
	lookup, err := bl.feedFactory.NewLookup(feeds.Sequence, f)
	if err != nil {
		logger.Error(err, "new lookup failed")
		return swarm.ZeroAddress, err
	}

	ch, _, _, err := lookup.At(ctx, at, after)
	if err != nil {
		logger.Error(err, "lookup at failed", "at", at)
		return swarm.ZeroAddress, err
	}

	// KLUDGE: if a feed was never updated, the chunk will be nil
	if ch == nil {
		logger.Error(nil, "no update found")
		return swarm.ZeroAddress, nil
	}

	wc, err := feeds.GetWrappedChunk(ctx, bl.storer.Download(false), ch)
	if err != nil {
		logger.Error(nil, "wrapped chunk cannot be retrieved")
		return swarm.ZeroAddress, nil
	}

	// curBytes, err := cur.MarshalBinary()
	// if err != nil {
	// 	logger.Debug("marshal current index failed", "error", err)
	// 	logger.Error(nil, "marshal current index failed")
	// 	return swarm.ZeroAddress, nil
	// }

	// nextBytes, err := next.MarshalBinary()
	// if err != nil {
	// 	logger.Debug("marshal next index failed", "error", err)
	// 	logger.Error(nil, "marshal next index failed")
	// 	return swarm.ZeroAddress, nil
	// }

	// socCh, err := soc.FromChunk(ch)
	// if err != nil {
	// 	logger.Error(nil, "wrapped chunk cannot be retrieved")
	// 	return swarm.ZeroAddress, nil
	// }
	// sig := socCh.Signature()

	// additionalHeaders := http.Header{
	// 	ContentTypeHeader:          {"application/octet-stream"},
	// 	SwarmFeedIndexHeader:       {hex.EncodeToString(curBytes)},
	// 	SwarmFeedIndexNextHeader:   {hex.EncodeToString(nextBytes)},
	// 	SwarmSocSignatureHeader:    {hex.EncodeToString(sig)},
	// 	AccessControlExposeHeaders: {SwarmFeedIndexHeader, SwarmFeedIndexNextHeader, SwarmSocSignatureHeader},
	// }

	// bl.downloadHandler(ctx, logger, wc.Address(), wc)

	return wc.Address(), nil
}

// // downloadHandler contains common logic for downloading Swarm file from API
// func (bl *Beelite) downloadHandler(ctx context.Context, logger log.Logger, reference swarm.Address, rootCh swarm.Chunk) {
// 	fallbackmode := false
// 	defstrat := getter.DefaultStrategy
// 	ctx, err := getter.SetConfigInContext(ctx, &defstrat, &fallbackmode, nil, logger)
// 	if err != nil {
// 		logger.Error(err, err.Error())
// 		return
// 	}
// 	rLevel := redundancy.DefaultLevel

// 	var (
// 		reader file.Joiner
// 		l      int64
// 	)
// 	if rootCh != nil {
// 		reader, l, err = joiner.NewJoiner(ctx, bl.storer.Download(false), bl.storer.Cache(), reference, rootCh)
// 	} else {
// 		reader, l, err = joiner.New(ctx, bl.storer.Download(false), bl.storer.Cache(), reference, rLevel)
// 	}
// 	if err != nil {
// 		logger.Error(err, "api download: error", "address", reference)
// 		return
// 	}

// 	bufSize := lookaheadBufferSize(l)
// 	if bufSize > 0 {
// 		http.ServeContent(w, r, "", time.Now(), langos.NewBufferedLangos(reader, bufSize))
// 		return
// 	}
// 	http.ServeContent(w, r, "", time.Now(), reader)
// }

const (
	smallFileBufferSize = 8 * 32 * 1024
	largeFileBufferSize = 16 * 32 * 1024

	largeBufferFilesizeThreshold = 10 * 1000000 // ten megs
)

func lookaheadBufferSize(size int64) int {
	if size <= largeBufferFilesizeThreshold {
		return smallFileBufferSize
	}
	return largeFileBufferSize
}

package beelite

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/ethersphere/bee/v2/pkg/cac"
	"github.com/ethersphere/bee/v2/pkg/postage"
	"github.com/ethersphere/bee/v2/pkg/soc"
	"github.com/ethersphere/bee/v2/pkg/storage"
	storer "github.com/ethersphere/bee/v2/pkg/storer"
	"github.com/ethersphere/bee/v2/pkg/swarm"
)

func (bl *Beelite) GetChunk(parentContext context.Context, reference swarm.Address, publisher *ecdsa.PublicKey, historyAddress *swarm.Address, timestamp *int64) (swarm.Chunk, error) {
	cache := true
	decryptedRef, err := bl.actDecryptionHandler(parentContext, reference, publisher, historyAddress, timestamp, cache)
	if err != nil {
		bl.logger.Error(err, "act decryption failed")
		return nil, err
	}
	chunk, err := bl.storer.Download(cache).Get(parentContext, decryptedRef)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			msg := fmt.Sprintf("chunk: chunk not found. addr %s", decryptedRef)
			bl.logger.Debug(msg)
			return nil, fmt.Errorf(msg)

		}
		return nil, fmt.Errorf("chunk: chunk read error: %v ,addr %s", err, decryptedRef)
	}
	return chunk, nil
}

func (bl *Beelite) AddChunk(parentContext context.Context,
	batchHex string,
	stampSig []byte,
	act bool,
	historyAddress swarm.Address,
	reader io.Reader,
	swarmTag uint64,
) (reference swarm.Address, newHistoryAddress swarm.Address, err error) {
	reference = swarm.ZeroAddress
	batch, err := hex.DecodeString(batchHex)
	if err != nil {
		err = errInvalidPostageBatch
		return
	}

	var (
		tag uint64
	)

	if swarmTag > 0 {
		tag, err = bl.getOrCreateSessionID(swarmTag)
		if err != nil {
			bl.logger.Error(err, "get or create tag failed")
			return
		}
	}
	deferred := tag != 0
	var putter storer.PutterSession
	if len(stampSig) != 0 {
		stamp := postage.Stamp{}
		if err = stamp.UnmarshalBinary(stampSig); err != nil {
			bl.logger.Error(err, "Stamp deserialization failure")
			return
		}

		putter, err = bl.newStampedPutter(parentContext, putterOptions{
			BatchID:  stamp.BatchID(),
			TagID:    tag,
			Deferred: deferred,
		}, &stamp)
	} else {
		putter, err = bl.newStamperPutter(parentContext, putterOptions{
			BatchID:  batch,
			TagID:    tag,
			Deferred: deferred,
		})
	}
	if err != nil {
		bl.logger.Error(err, "get putter failed")
		return
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		bl.logger.Error(err, "chunk upload: read chunk data failed")
		return

	}

	if len(data) < swarm.SpanSize {
		err = errors.New("insufficient data length")
		bl.logger.Error(err, "chunk upload: insufficient data length")
		return
	}

	chunk, err := cac.NewWithDataSpan(data)
	if err != nil {
		// not a valid cac chunk. Check if it's a replica soc chunk.
		bl.logger.Debug("chunk upload: create chunk failed", "error", err)

		// FromChunk only uses the chunk data to recreate the soc chunk. So the address is irrelevant.
		var sch *soc.SOC
		sch, err = soc.FromChunk(swarm.NewChunk(swarm.EmptyAddress, data))
		if err != nil {
			bl.logger.Error(err, "chunk upload: create chunk error")
			return
		}
		chunk, err = sch.Chunk()
		if err != nil {
			bl.logger.Error(nil, "chunk upload: create chunk error")
			return
		}

		if !soc.Valid(chunk) {
			bl.logger.Error(nil, "chunk upload: create chunk error")
			return
		}
	}

	reference = chunk.Address()
	if act {
		reference, newHistoryAddress, err = bl.actEncryptionHandler(parentContext, putter, reference, historyAddress)
		if err != nil {
			bl.logger.Error(err, "access control upload failed")
			return
		}
	}

	err = putter.Put(parentContext, chunk)
	if err != nil {
		bl.logger.Error(err, "chunk upload: write chunk failed", "chunk_address", chunk.Address())
		return
	}

	err = putter.Done(chunk.Address())
	if err != nil {
		bl.logger.Error(err, "done split failed")
		err = errors.Join(fmt.Errorf("done split failed: %w", err), putter.Cleanup())
		return
	}

	return
}

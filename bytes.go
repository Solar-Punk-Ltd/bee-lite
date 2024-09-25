package beelite

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/ethersphere/bee/v2/pkg/file/joiner"
	"github.com/ethersphere/bee/v2/pkg/file/redundancy"
	"github.com/ethersphere/bee/v2/pkg/storage"
	"github.com/ethersphere/bee/v2/pkg/swarm"
)

func (bl *Beelite) AddBytes(parentContext context.Context,
	batchHex string,
	act bool,
	historyAddress swarm.Address,
	encrypt bool,
	rLevel redundancy.Level,
	reader io.Reader,
) (reference swarm.Address, newHistoryAddress swarm.Address, err error) {
	reference = swarm.ZeroAddress
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
	putter, err := bl.newStamperPutter(parentContext, putterOptions{
		BatchID:  batch,
		TagID:    tag,
		Pin:      pin,
		Deferred: deferred,
	})
	if err != nil {
		bl.logger.Error(err, "get putter failed")
		return
	}

	p := requestPipelineFn(putter, encrypt, rLevel)
	reference, err = p(parentContext, reader)
	if err != nil {
		err = fmt.Errorf("(split write all) upload failed 1: %w", err)
		return
	}

	encryptedReference := reference
	if act {
		encryptedReference, newHistoryAddress, err = bl.actEncryptionHandler(parentContext, putter, reference, historyAddress)
		if err != nil {
			bl.logger.Error(err, "access control upload failed")
			return
		}
	}

	err = putter.Done(reference)
	if err != nil {
		bl.logger.Error(err, "done split failed")
		err = errors.Join(fmt.Errorf("(done split) upload failed 2: %w", err), putter.Cleanup())
		return
	}

	reference = encryptedReference
	return
}

func (bl *Beelite) GetBytes(parentContext context.Context, reference swarm.Address, publisher *ecdsa.PublicKey, historyAddress *swarm.Address, timestamp *int64) (io.Reader, error) {
	cache := true
	decryptedRef, err := bl.actDecryptionHandler(parentContext, reference, publisher, historyAddress, timestamp, cache)
	if err != nil {
		bl.logger.Error(err, "act decryption failed")
		return nil, err
	}
	reader, _, err := joiner.New(parentContext, bl.storer.Download(cache), bl.storer.Cache(), decryptedRef)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, fmt.Errorf("api download: not found : %w", err)
		}
		return nil, fmt.Errorf("unexpected error: %v: %v", decryptedRef, err)
	}
	return reader, nil
}

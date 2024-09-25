package beelite

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/ethersphere/bee/v2/pkg/cac"
	"github.com/ethersphere/bee/v2/pkg/postage"
	"github.com/ethersphere/bee/v2/pkg/soc"
	storer "github.com/ethersphere/bee/v2/pkg/storer"
	"github.com/ethersphere/bee/v2/pkg/swarm"
)

const (
	TopicLength = 32
)

type Topic [TopicLength]byte

func (bl *Beelite) AddSOC(ctx context.Context,
	batchHex string,
	stampSig []byte,
	act bool,
	historyAddress swarm.Address,
	reader io.Reader,
	id []byte,
	owner []byte,
	sig []byte,
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
		tag uint64
		pin = false
	)

	// if pinning header is set we do a deferred upload, else we do a direct upload
	if pin {
		tag, err = bl.getOrCreateSessionID(uint64(0))
		if err != nil {
			bl.logger.Error(err, "get or create tag failed")
			return
		}
	}
	var putter storer.PutterSession
	if len(stampSig) != 0 {
		stamp := postage.Stamp{}
		if err = stamp.UnmarshalBinary(stampSig); err != nil {
			bl.logger.Error(err, "Stamp deserialization failure")
			return
		}

		putter, err = bl.newStampedPutter(ctx, putterOptions{
			BatchID:  stamp.BatchID(),
			TagID:    tag,
			Pin:      pin,
			Deferred: pin,
		}, &stamp)
	} else {
		putter, err = bl.newStamperPutter(ctx, putterOptions{
			BatchID:  batch,
			TagID:    tag,
			Pin:      pin,
			Deferred: pin,
		})
	}
	if err != nil {
		bl.logger.Error(err, "get putter failed")
		return
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		bl.logger.Error(err, "soc upload: read chunk data failed")
		return

	}

	if len(data) < swarm.SpanSize {
		err = errors.New("chunk data too short")
		bl.logger.Error(err, "soc upload: chunk data too short")
		return
	}

	if len(data) > swarm.ChunkSize+swarm.SpanSize {
		err = errors.New("chunk data exceeds required length")
		bl.logger.Error(err, "required_length", swarm.ChunkSize+swarm.SpanSize)
		return
	}

	chunk, err := cac.NewWithDataSpan(data)
	if err != nil {
		bl.logger.Error(err, "soc upload: create content addressed chunk failed")
		return
	}

	ss, err := soc.NewSigned(id, chunk, owner, sig)
	if err != nil {
		bl.logger.Error(err, "create soc failed", "id", id, "owner", owner, "error", err)
		return
	}

	sch, err := ss.Chunk()
	if err != nil {
		bl.logger.Error(err, "read chunk data failed", "error")
		return
	}

	if !soc.Valid(sch) {
		bl.logger.Error(nil, "invalid chunk", "error")
		return swarm.ZeroAddress, swarm.ZeroAddress, nil
	}

	reference = sch.Address()
	if act {
		reference, newHistoryAddress, err = bl.actEncryptionHandler(ctx, putter, reference, historyAddress)
		if err != nil {
			bl.logger.Error(err, "access control upload failed")
			return
		}
	}

	err = putter.Put(ctx, sch)
	if err != nil {
		bl.logger.Error(err, "soc upload: write chunk failed", "chunk_address", chunk.Address())
		return
	}

	err = putter.Done(sch.Address())
	if err != nil {
		bl.logger.Error(err, "done split failed")
		err = errors.Join(fmt.Errorf("done split failed: %w", err), putter.Cleanup())
		return
	}

	return
}

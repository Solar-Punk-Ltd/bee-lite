package beelite

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/ethersphere/bee/v2/pkg/crypto"
	"github.com/ethersphere/bee/v2/pkg/file/loadsave"
	"github.com/ethersphere/bee/v2/pkg/file/redundancy"
	"github.com/ethersphere/bee/v2/pkg/swarm"
)

func (bl *Beelite) GetGranteeList(ctx context.Context, encryptedglRef swarm.Address, cache bool) ([]string, error) {
	publisher := bl.publicKey
	ls := loadsave.NewReadonly(bl.storer.Download(cache))
	grantees, err := bl.accesscontrol.Get(ctx, ls, publisher, encryptedglRef)
	if err != nil {
		bl.logger.Error(err, "could not get grantees")
		return nil, err
	}
	granteeSlice := make([]string, len(grantees))
	for i, grantee := range grantees {
		granteeSlice[i] = hex.EncodeToString(crypto.EncodeSecp256k1PublicKey(grantee))
	}

	return granteeSlice, nil
}

func (bl *Beelite) AddRevokeGrantees(ctx context.Context, batchHex string, granteesAddress swarm.Address, historyAddress swarm.Address, addlist, revokelist []string) (swarm.Address, swarm.Address, error) {
	if addlist == nil && revokelist == nil {
		err := fmt.Errorf("nothing to add or remove")
		bl.logger.Error(err, "nothig to add or remove")
		return swarm.ZeroAddress, swarm.ZeroAddress, err
	}

	var (
		tag      uint64
		err      error
		deferred = false
		pin      = false
	)

	if deferred || pin {
		tag, err = bl.getOrCreateSessionID(uint64(0))
		if err != nil {
			bl.logger.Error(err, "get or create tag failed")
			return swarm.ZeroAddress, swarm.ZeroAddress, err
		}
	}

	parsedAddlist, err := parseKeys(addlist)
	if err != nil {
		bl.logger.Error(err, "add list key parse failed")
		return swarm.ZeroAddress, swarm.ZeroAddress, err
	}

	parsedRevokelist, err := parseKeys(revokelist)
	if err != nil {
		bl.logger.Error(err, "revoke list key parse failed")
		return swarm.ZeroAddress, swarm.ZeroAddress, err
	}

	batch, err := hex.DecodeString(batchHex)
	if err != nil {
		return swarm.ZeroAddress, swarm.ZeroAddress, errInvalidPostageBatch
	}
	putter, err := bl.newStamperPutter(ctx, putterOptions{
		BatchID:  batch,
		TagID:    tag,
		Pin:      pin,
		Deferred: deferred,
	})
	if err != nil {
		bl.logger.Error(err, "putter failed")
		return swarm.ZeroAddress, swarm.ZeroAddress, err
	}

	granteeref := granteesAddress
	publisher := bl.publicKey
	ls := loadsave.New(bl.storer.Download(true), bl.storer.Cache(), requestPipelineFactory(ctx, putter, false, redundancy.NONE))
	gls := loadsave.New(bl.storer.Download(true), bl.storer.Cache(), requestPipelineFactory(ctx, putter, true, redundancy.NONE))
	granteeref, encryptedglref, historyref, actref, err := bl.accesscontrol.UpdateHandler(ctx, ls, gls, granteeref, historyAddress, publisher, parsedAddlist, parsedRevokelist)
	if err != nil {
		bl.logger.Error(err, "failed to update grantee list")
		return swarm.ZeroAddress, swarm.ZeroAddress, err
	}

	err = putter.Done(actref)
	if err != nil {
		bl.logger.Error(err, "done split act failed")
		return swarm.ZeroAddress, swarm.ZeroAddress, err
	}

	err = putter.Done(historyref)
	if err != nil {
		bl.logger.Error(err, "done split history failed")
		return swarm.ZeroAddress, swarm.ZeroAddress, err
	}

	err = putter.Done(granteeref)
	if err != nil {
		bl.logger.Error(err, "done split grantees failed")
		return swarm.ZeroAddress, swarm.ZeroAddress, err
	}

	return encryptedglref, historyref, nil
}

func (bl *Beelite) CreateGrantees(ctx context.Context, batchHex string, historyAddress swarm.Address, granteeList []string) (swarm.Address, swarm.Address, error) {
	if granteeList == nil {
		err := fmt.Errorf("nothing to create")
		bl.logger.Error(err, "nothig to create")
		return swarm.ZeroAddress, swarm.ZeroAddress, err
	}

	var (
		tag      uint64
		err      error
		deferred = false
		pin      = false
	)

	batch, err := hex.DecodeString(batchHex)
	if err != nil {
		return swarm.ZeroAddress, swarm.ZeroAddress, errInvalidPostageBatch
	}
	putter, err := bl.newStamperPutter(ctx, putterOptions{
		BatchID:  batch,
		TagID:    tag,
		Pin:      pin,
		Deferred: deferred,
	})
	if err != nil {
		bl.logger.Error(err, "putter failed")
		return swarm.ZeroAddress, swarm.ZeroAddress, err
	}

	list, err := parseKeys(granteeList)
	if err != nil {
		bl.logger.Error(nil, "create list key parse failed")
		return swarm.ZeroAddress, swarm.ZeroAddress, err
	}

	publisher := bl.publicKey
	ls := loadsave.New(bl.storer.Download(true), bl.storer.Cache(), requestPipelineFactory(ctx, putter, false, redundancy.NONE))
	gls := loadsave.New(bl.storer.Download(true), bl.storer.Cache(), requestPipelineFactory(ctx, putter, true, redundancy.NONE))
	granteeref, encryptedglref, historyref, actref, err := bl.accesscontrol.UpdateHandler(ctx, ls, gls, swarm.ZeroAddress, historyAddress, publisher, list, nil)
	if err != nil {
		bl.logger.Error(nil, "failed to create grantee list")
		return swarm.ZeroAddress, swarm.ZeroAddress, err
	}

	err = putter.Done(actref)
	if err != nil {
		bl.logger.Error(nil, "done split act failed")
		return swarm.ZeroAddress, swarm.ZeroAddress, err
	}

	err = putter.Done(historyref)
	if err != nil {
		bl.logger.Error(nil, "done split history failed")
		return swarm.ZeroAddress, swarm.ZeroAddress, err
	}

	err = putter.Done(granteeref)
	if err != nil {
		bl.logger.Error(nil, "done split grantees failed")
		return swarm.ZeroAddress, swarm.ZeroAddress, err
	}

	return encryptedglref, historyref, nil
}

func parseKeys(list []string) ([]*ecdsa.PublicKey, error) {
	parsedList := make([]*ecdsa.PublicKey, 0, len(list))
	for _, g := range list {
		h, err := hex.DecodeString(g)
		if err != nil {
			return []*ecdsa.PublicKey{}, fmt.Errorf("failed to decode grantee: %w", err)
		}
		k, err := btcec.ParsePubKey(h)
		if err != nil {
			return []*ecdsa.PublicKey{}, fmt.Errorf("failed to parse grantee public key: %w", err)
		}
		parsedList = append(parsedList, k.ToECDSA())
	}

	return parsedList, nil
}

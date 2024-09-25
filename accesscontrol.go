package beelite

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"time"

	"github.com/ethersphere/bee/v2/pkg/file/loadsave"
	"github.com/ethersphere/bee/v2/pkg/file/redundancy"
	"github.com/ethersphere/bee/v2/pkg/storer"
	"github.com/ethersphere/bee/v2/pkg/swarm"
)

func (bl *Beelite) actDecryptionHandler(ctx context.Context, reference swarm.Address, publisher *ecdsa.PublicKey, historyAddress *swarm.Address, timestamp *int64, cache bool) (swarm.Address, error) {
	// Try to download the file wihthout decryption, if the act headers are not present
	if publisher == nil || historyAddress == nil {
		return reference, nil
	}

	ts := time.Now().Unix()
	if timestamp != nil {
		ts = *timestamp
	}

	ls := loadsave.NewReadonly(bl.storer.Download(cache))
	decryptedRef, err := bl.accesscontrol.DownloadHandler(ctx, ls, reference, publisher, *historyAddress, ts)
	if err != nil {
		bl.logger.Error(err, "access control download failed")
		return swarm.ZeroAddress, err
	}

	return decryptedRef, nil
}

func (bl *Beelite) actEncryptionHandler(
	ctx context.Context,
	putter storer.PutterSession,
	reference swarm.Address,
	historyRootHash swarm.Address,
) (swarm.Address, swarm.Address, error) {
	publisherPublicKey := bl.publicKey
	ls := loadsave.New(bl.storer.Download(true), bl.storer.Cache(), requestPipelineFactory(ctx, putter, false, redundancy.NONE))
	storageReference, historyReference, encryptedReference, err := bl.accesscontrol.UploadHandler(ctx, ls, reference, publisherPublicKey, historyRootHash)
	if err != nil {
		return swarm.ZeroAddress, swarm.ZeroAddress, err
	}
	// only need to upload history and kvs if a new history is created,
	// meaning that the publisher uploaded to the history for the first time
	if !historyReference.Equal(historyRootHash) {
		err = putter.Done(storageReference)
		if err != nil {
			return swarm.ZeroAddress, swarm.ZeroAddress, fmt.Errorf("done split key-value store failed: %w", err)
		}
		err = putter.Done(historyReference)
		if err != nil {
			return swarm.ZeroAddress, swarm.ZeroAddress, fmt.Errorf("done split history failed: %w", err)
		}
	}

	return encryptedReference, historyReference, nil
}

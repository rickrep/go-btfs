package upload

import (
	"context"
	"fmt"

	"github.com/TRON-US/go-btfs/core/commands/storage/upload/escrow"
	"github.com/TRON-US/go-btfs/core/commands/storage/upload/sessions"

	config "github.com/TRON-US/go-btfs-config"
	escrowpb "github.com/tron-us/go-btfs-common/protos/escrow"
	"github.com/tron-us/go-btfs-common/utils/grpc"
	"github.com/tron-us/protobuf/proto"
)

func Submit(rss *sessions.RenterSession, fileSize int64, offlineSigning bool) error {
	if err := rss.To(sessions.RssToSubmitEvent); err != nil {
		return err
	}
	res, err := doSubmit(rss, offlineSigning)
	if err != nil {
		return err
	}
	return pay(rss, res, fileSize, offlineSigning)
}

func doSubmit(rss *sessions.RenterSession, offlineSigning bool) (*escrowpb.SignedSubmitContractResult, error) {
	bs, t, err := prepareContracts(rss, rss.ShardHashes)
	if err != nil {
		return nil, err
	}
	err = checkBalance(rss, offlineSigning, t)
	if err != nil {
		return nil, err
	}
	req, err := NewContractRequest(rss, bs, t, offlineSigning)
	if err != nil {
		return nil, err
	}
	var amount int64 = 0
	for _, c := range req.Contract {
		amount += c.Contract.Amount
	}
	submitContractRes, err := submitContractToEscrow(rss.Ctx, rss.CtxParams.Cfg, req)
	if err != nil {
		return nil, err
	}
	return submitContractRes, nil
}

func prepareContracts(rss *sessions.RenterSession, shardHashes []string) ([]*escrowpb.SignedEscrowContract, int64, error) {
	var signedContracts []*escrowpb.SignedEscrowContract
	var totalPrice int64
	for i, hash := range shardHashes {
		shard, err := sessions.GetRenterShard(rss.CtxParams, rss.SsId, hash, i)
		if err != nil {
			return nil, 0, err
		}
		c, err := shard.Contracts()
		if err != nil {
			return nil, 0, err
		}
		escrowContract := &escrowpb.SignedEscrowContract{}
		err = proto.Unmarshal(c.SignedEscrowContract, escrowContract)
		if err != nil {
			return nil, 0, err
		}
		signedContracts = append(signedContracts, escrowContract)
		totalPrice += c.SignedGuardContract.Amount
	}
	return signedContracts, totalPrice, nil
}

func submitContractToEscrow(ctx context.Context, configuration *config.Config,
	request *escrowpb.EscrowContractRequest) (*escrowpb.SignedSubmitContractResult, error) {
	var (
		response *escrowpb.SignedSubmitContractResult
		err      error
	)
	err = grpc.EscrowClient(configuration.Services.EscrowDomain).WithContext(ctx,
		func(ctx context.Context, client escrowpb.EscrowServiceClient) error {
			response, err = client.SubmitContracts(ctx, request)
			if err != nil {
				return err
			}
			if response == nil {
				return fmt.Errorf("escrow reponse is nil")
			}
			// verify
			err = escrow.VerifyEscrowRes(configuration, response.Result, response.EscrowSignature)
			if err != nil {
				return fmt.Errorf("verify escrow failed %v", err)
			}
			return nil
		})
	return response, err
}

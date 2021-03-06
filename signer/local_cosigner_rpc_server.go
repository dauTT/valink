package signer

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/tendermint/tendermint/libs/log"
	tmnet "github.com/tendermint/tendermint/libs/net"
	"github.com/tendermint/tendermint/libs/service"
	grpc "google.golang.org/grpc"
)

type CosignerRpcServerConfig struct {
	Logger        log.Logger
	ListenAddress string
	LocalCosigner Cosigner
	Peers         []RemoteCosigner
}

// CosignerRpcServer responds to rpc sign requests using a cosigner instance
type CosignerRpcServer struct {
	service.BaseService

	logger        log.Logger
	listenAddress string
	listener      net.Listener
	localCosigner Cosigner
	peers         []RemoteCosigner
}

// NewCosignerRpcServer instantiates a local cosigner with the specified key and sign state
func NewCosignerRpcServer(config *CosignerRpcServerConfig) *CosignerRpcServer {
	cosignerRpcServer := &CosignerRpcServer{
		localCosigner: config.LocalCosigner,
		listenAddress: config.ListenAddress,
		peers:         config.Peers,
		logger:        config.Logger,
	}

	cosignerRpcServer.BaseService = *service.NewBaseService(config.Logger, "CosignerRpcServer", cosignerRpcServer)
	return cosignerRpcServer
}

// OnStart starts the rpm server to respond to remote CosignerSignRequests
func (rpcServer *CosignerRpcServer) OnStart() error {
	proto, address := tmnet.ProtocolAndAddress(rpcServer.listenAddress)

	lis, err := net.Listen(proto, address)
	if err != nil {
		return err
	}
	rpcServer.listener = lis

	grpcServer := grpc.NewServer()

	RegisterCosignerServiceServer(grpcServer, rpcServer)

	// tcpLogger := rpcServer.Logger.With("socket", "tcp")
	// tcpLogger = log.NewFilter(tcpLogger, log.AllowError())
	// config := server.DefaultConfig()

	go func() {
		defer lis.Close()
		//server.Serve(lis, tcpLogger, config)
		if err := grpcServer.Serve(lis); err != nil {
			rpcServer.logger.Error("failed to serve", "error", err)
		}
	}()

	return nil
}

func (rpcServer *CosignerRpcServer) Addr() net.Addr {
	if rpcServer.listener == nil {
		return nil
	}
	return rpcServer.listener.Addr()
}

func (rpcServer *CosignerRpcServer) Sign(ctx context.Context, req *CosignerSignRequest) (*CosignerSignResponse, error) {
	//rpcServer.logger.Info("rpcSignRequest", "from=")

	response := &CosignerSignResponse{}

	height, round, step, err := UnpackHRS(req.SignBytes)
	if err != nil {
		return response, err
	}

	wg := sync.WaitGroup{}
	wg.Add(len(rpcServer.peers))

	// ping peers for our ephemeral share part
	for _, peer := range rpcServer.peers {
		request := func(peer RemoteCosigner) {

			// need to do these requests in parallel..!!

			// RPC requests are blocking
			// to prevent it from hanging our process indefinitely, we use a timeout context and a goroutine
			partReqCtx, partReqCtxCancel := context.WithTimeout(context.Background(), time.Second)

			go func() {
				partRequest := CosignerGetEphemeralSecretPartRequest{
					ID:     int32(rpcServer.localCosigner.GetID()),
					Height: height,
					Round:  round,
					Step:   int32(step),
				}

				// if we already have an ephemeral secret part for this HRS, we don't need to re-query for it
				hasResp, err := rpcServer.localCosigner.HasEphemeralSecretPart(CosignerHasEphemeralSecretPartRequest{
					ID:     peer.GetID(),
					Height: height,
					Round:  round,
					Step:   step,
				})

				if err != nil {
					rpcServer.logger.Error("HasEphemeralSecretPart req error", "error", err)
					return
				}

				if hasResp.Exists {
					partReqCtxCancel()
					return
				}

				partResponse, err := peer.GetEphemeralSecretPart(&partRequest)
				if err != nil {
					rpcServer.logger.Error("GetEphemeralSecretPart req error", "error", err)
					return
				}

				// no need to contine if timed out
				select {
				case <-partReqCtx.Done():
					return
				default:
				}

				defer partReqCtxCancel()

				// set the share part from the response
				err = rpcServer.localCosigner.SetEphemeralSecretPart(CosignerSetEphemeralSecretPartRequest{
					SourceID:                       int(partResponse.SourceID),
					SourceEphemeralSecretPublicKey: partResponse.SourceEphemeralSecretPublicKey,
					EncryptedSharePart:             partResponse.EncryptedSharePart,
					Height:                         height,
					Round:                          round,
					Step:                           step,
					SourceSig:                      partResponse.SourceSig,
				})
				if err != nil {
					rpcServer.logger.Error("SetEphemeralSecretPart req error", "error", err)
				}
			}()

			// wait for timeout or done
			select {
			case <-partReqCtx.Done():
			}

			wg.Done()
		}

		go request(peer)
	}

	wg.Wait()

	// after getting any share parts we could, we sign
	resp, err := rpcServer.localCosigner.Sign(&CosignerSignRequest{
		SignBytes: req.SignBytes,
	})
	if err != nil {
		return response, err
	}

	response.Timestamp = resp.Timestamp
	response.Signature = resp.Signature
	return response, nil
}

func (rpcServer *CosignerRpcServer) GetEphemeralSecretPart(ctx context.Context, req *CosignerGetEphemeralSecretPartRequest) (*CosignerGetEphemeralSecretPartResponse, error) {
	response := &CosignerGetEphemeralSecretPartResponse{}

	partResp, err := rpcServer.localCosigner.GetEphemeralSecretPart(&CosignerGetEphemeralSecretPartRequest{
		ID:     req.ID,
		Height: req.Height,
		Round:  req.Round,
		Step:   req.Step,
	})
	if err != nil {
		//rpcServer.logger.Info("NO RESPONSE from : ", ctx.RemoteAddr())
		return response, nil
	}

	response.SourceID = partResp.SourceID
	response.SourceEphemeralSecretPublicKey = partResp.SourceEphemeralSecretPublicKey
	response.EncryptedSharePart = partResp.EncryptedSharePart
	response.SourceSig = partResp.SourceSig

	return response, nil
}

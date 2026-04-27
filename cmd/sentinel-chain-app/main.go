// sentinel-chain-app is the CometBFT application for the Sentinel governance chain.
//
// It implements the ABCI application interface so that the chain only accepts
// valid sentinel.anchor.v1 transactions. Full payloads are never stored here —
// only packet hashes, decision hashes, policy bundle hashes, actor identity hashes,
// and receipt metadata.
//
// Run alongside a CometBFT node; the two communicate over the ABCI socket.
//
// Reference: https://docs.cometbft.com/main/tutorials/go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"
)

// AnchorTx mirrors the wire format submitted by the anchor queue.
type AnchorTx struct {
	TxType           string `json:"tx_type"`
	PacketID         string `json:"packet_id"`
	PacketHash       string `json:"packet_hash"`
	DecisionHash     string `json:"decision_hash"`
	PolicyBundleHash string `json:"policy_bundle_hash"`
	AppID            string `json:"app_id"`
	Risk             string `json:"risk"`
	AnchorMode       string `json:"anchor_mode"`
	Signature        string `json:"signature"`
}

func main() {
	log, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sentinel-chain-app: failed to init logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Sync() //nolint:errcheck

	log.Info("sentinel-chain-app starting (CometBFT ABCI stub)")

	// TODO M4: implement cometbft/abci Application interface.
	//   - DeliverTx: validate AnchorTx, verify signature, store in state DB.
	//   - Query: return receipt data by packet ID.
	//   - CheckTx: validate tx format without applying state.
	//   - Commit: persist state root hash.

	// Validation stub — confirm the AnchorTx shape is correct.
	sample := AnchorTx{
		TxType:           "sentinel.anchor.v1",
		PacketID:         "pkt_example",
		PacketHash:       "sha256:abc",
		DecisionHash:     "sha256:def",
		PolicyBundleHash: "sha256:ghi",
		AppID:            "billing-api",
		Risk:             "high",
		AnchorMode:       "immediate",
		Signature:        "ed25519:placeholder",
	}
	b, _ := json.MarshalIndent(sample, "", "  ")
	log.Info("sample anchor tx", zap.String("tx", string(b)))

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info("sentinel-chain-app shutting down")
}

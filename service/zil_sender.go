package service

import (
	"encoding/hex"
	"encoding/json"
	"github.com/Zilliqa/gozilliqa-sdk/crosschain/polynetwork"
	"github.com/Zilliqa/gozilliqa-sdk/provider"
	"github.com/Zilliqa/gozilliqa-sdk/util"
	"github.com/ontio/ontology-crypto/keypair"
	"github.com/ontio/ontology-crypto/signature"
	poly_go_sdk "github.com/polynetwork/poly-go-sdk"
	vconfig "github.com/polynetwork/poly/consensus/vbft/config"
	polytypes "github.com/polynetwork/poly/core/types"
	common2 "github.com/polynetwork/poly/native/service/cross_chain_manager/common"
	"github.com/polynetwork/zilliqa-relayer/config"
	"github.com/polynetwork/zilliqa-relayer/tools"
	log "github.com/sirupsen/logrus"
)

type ZilSender struct {
	cfg        *config.Config
	zilSdk     *provider.Provider
	address    string //non-bech32 address
	privateKey string

	polySdk         *poly_go_sdk.PolySdk
	crossChainProxy *polynetwork.Proxy
}

func (sender *ZilSender) commitDepositEventsWithHeader(header *polytypes.Header, param *common2.ToMerkleValue, headerProof string, anchorHeader *polytypes.Header, polyTxHash string, rawAuditPath []byte) bool {
	// verifyHeaderAndExecuteTx
	var (
		sigs       []byte
		headerData []byte
	)
	if anchorHeader != nil && headerProof != "" {
		for _, sig := range anchorHeader.SigData {
			temp := make([]byte, len(sig))
			copy(temp, sig)
			newsig, _ := signature.ConvertToEthCompatible(temp)
			sigs = append(sigs, newsig...)
		}
	} else {
		for _, sig := range header.SigData {
			temp := make([]byte, len(sig))
			copy(temp, sig)
			newsig, _ := signature.ConvertToEthCompatible(temp)
			sigs = append(sigs, newsig...)
		}
	}

	// todo ensure that TxHash is bytes of hash, not utf8 bytes
	exist := sender.checkIfFromChainTxExist(param.FromChainID, util.EncodeHex(param.TxHash))
	if exist {
		log.Infof("ZilSender commitDepositEventsWithHeader - already relayed to zil: (from_chain_id: %d, from_txhash: %x, param.TxHash: %x\n)", param.FromChainID, param.TxHash, param.MakeTxParam.TxHash)
		return true
	}

	var rawAnchor []byte
	if anchorHeader != nil {
		rawAnchor = anchorHeader.GetMessage()
	}
	headerData = header.GetMessage()

	// todo carefully test this
	pe := polynetwork.DeserializeProof(util.EncodeHex(rawAuditPath), 0)
	rawHeader := util.EncodeHex(headerData)
	hpe := polynetwork.DeserializeProof(headerProof, 0)
	curRawHeader := util.EncodeHex(rawAnchor)
	signatures, _ := polynetwork.SplitSignature(util.EncodeHex(sigs))

	// todo use chan to handle result
	transaction, err := sender.crossChainProxy.VerifyHeaderAndExecuteTx(pe, rawHeader, hpe, curRawHeader, signatures)
	if err != nil {
		log.Errorf("ZilSender commitDepositEventsWithHeader - failed to call VerifyHeaderAndExecuteTx: %s\n", err.Error())
		return false
	}

	log.Infof("ZilSender commitDepositEventsWithHeader -  confirmed transaction: %s\n", transaction.ID)
	return true

}

func (sender *ZilSender) commitHeader(hdr *polytypes.Header) bool {
	headerdata := hdr.GetMessage()
	var (
		bookkeepers []keypair.PublicKey
		sigs        []byte
	)

	for _, sig := range hdr.SigData {
		temp := make([]byte, len(sig))
		copy(temp, sig)
		newsig, _ := signature.ConvertToEthCompatible(temp)
		sigs = append(sigs, newsig...)
	}

	blkInfo := &vconfig.VbftBlockInfo{}
	if err := json.Unmarshal(hdr.ConsensusPayload, blkInfo); err != nil {
		log.Errorf("commitHeader - unmarshal blockInfo error: %s", err)
		return false
	}

	for _, peer := range blkInfo.NewChainConfig.Peers {
		keystr, _ := hex.DecodeString(peer.ID)
		key, _ := keypair.DeserializePublicKey(keystr)
		bookkeepers = append(bookkeepers, key)
	}

	bookkeepers = keypair.SortPublicKeys(bookkeepers)
	publickeys := make([]byte, 0)
	for _, key := range bookkeepers {
		publickeys = append(publickeys, tools.GetNoCompresskey(key)...)
	}

	// todo carefully test this, add more useful logs
	rawHeader := util.EncodeHex(headerdata)
	PubKeys, _ := polynetwork.SplitPubKeys(util.EncodeHex(publickeys))
	signatures, _ := polynetwork.SplitSignature(util.EncodeHex(sigs))
	transaction, err := sender.crossChainProxy.ChangeBookKeeper(rawHeader, PubKeys, signatures)
	if err != nil {
		log.Errorf("ZilSender commitHeader - failed to call VerifyHeaderAndExecuteTx: %s\n", err.Error())
		return false
	}

	log.Infof("ZilSender commitHeader -  confirmed transaction: %s\n", transaction.ID)
	return true

}

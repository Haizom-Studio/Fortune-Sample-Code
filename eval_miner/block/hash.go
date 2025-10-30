package block

import (
	"crypto/sha256"
	"encoding/hex"
)

func CalcMerkleRootHash(CoinBaseHash []byte, MerkelBranch []string) []byte {
	/*
		Calculate Merkle Root Hash
		prevh = hash(coinbase, merklebranch[0])
		prevh = hash(prevh, merklebranch[1])
		...
		mrh = hash(prevh, merklebranch[n])
		hash is double sha256
	*/

	hash0 := CoinBaseHash
	for _, s := range MerkelBranch {
		hash := sha256.New()
		hash.Write(hash0)
		data, _ := hex.DecodeString(s)
		hash.Write(data)
		sum := hash.Sum(nil)
		sum2 := sha256.Sum256([]byte(sum[:]))
		copy(hash0, []byte(sum2[:]))
	}
	return hash0
}

func CalcCoinBaseHash(CoinB1 string, ExtraNonce1 string, Nonce2 string, CoinB2 string) [32]byte {
	/*
		//coinbase transaction double hash
		SHA256_Update(&sha256,ctx->coinb1, ctx->coinb1_len);
		SHA256_Update(&sha256,ctx->enonce1, ctx->enonce1_len);
		SHA256_Update(&sha256,enonce2, ctx->enonce2_len);
		SHA256_Update(&sha256,ctx->coinb2, ctx->coinb2_len);
		SHA256_Final(hash, &sha256);
		SHA256_Init(&sha256);
		SHA256_Update(&sha256,hash, sizeof(hash));
		SHA256_Final(hash, &sha256);

	*/
	CoinB1_Data, _ := hex.DecodeString(CoinB1)
	ExtraNonce1_Data, _ := hex.DecodeString(ExtraNonce1)
	Nonce2_Data, _ := hex.DecodeString(Nonce2)
	CoinB2_Data, _ := hex.DecodeString(CoinB2)

	CoinBaseHash := sha256.New()
	CoinBaseHash.Write(CoinB1_Data)
	CoinBaseHash.Write(ExtraNonce1_Data)
	CoinBaseHash.Write(Nonce2_Data)
	CoinBaseHash.Write(CoinB2_Data)
	CoinBaseSum := CoinBaseHash.Sum(nil)
	CoinBaseSum2 := sha256.Sum256([]byte(CoinBaseSum[:]))
	return CoinBaseSum2
}

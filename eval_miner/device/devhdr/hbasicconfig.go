package devhdr

var DefaultHbAsicIdConfig = &HashBoardAsicIdConfig{
	ChipsLow: struct {
		Low  int
		High int
	}{
		Low:  0,
		High: 65,
	},
	ChipsHi: struct {
		Low  int
		High int
	}{
		Low:  128,
		High: 193,
	},
}

type HashBoardAsicIdConfig struct {
	ChipsLow struct {
		Low  int
		High int
	}
	ChipsHi struct {
		Low  int
		High int
	}
}

func ChipIDtoIndex(hbConfig *HashBoardAsicIdConfig, chipId uint8) int {
	// chipId is greater than max asics  [i.e. greater than 193 for default cards]
	// or is in unused range [i.e. greater than 65 and less than 128]
	if int(chipId) > hbConfig.ChipsHi.High ||
		(int(chipId) > hbConfig.ChipsLow.High && int(chipId) < hbConfig.ChipsHi.Low) {
		return -1 // This will generally cause a program crash as an invalid array index
	}
	if int(chipId) >= hbConfig.ChipsHi.Low {
		return int(chipId) - hbConfig.ChipsHi.Low + hbConfig.ChipsLow.High + 1 // Offset for chips 128 to 193
	}
	return int(chipId) // Offset for chips 0 to 65
}

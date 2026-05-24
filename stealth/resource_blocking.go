package stealth

// ResourceBlocking toggles default network resource classes to block.
type ResourceBlocking struct {
	BlockImages bool
	BlockFonts  bool
	BlockMedia  bool
}

func DefaultResourceBlocking() ResourceBlocking {
	return ResourceBlocking{BlockImages: true, BlockFonts: true, BlockMedia: false}
}

package slime

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/Tnze/go-mc/nbt"
	"github.com/klauspost/compress/zstd"
)

const (
	SlimeMagic      = 0xB10B
	SlimeVersionMin = 0x0C // v12
	SlimeVersionMax = 0x0D // v13

	FlagPOIChunks  = 1
	FlagFluidTicks = 2
	FlagBlockTicks = 4
)

// SlimeWorld represents a parsed slime world.
type SlimeWorld struct {
	WorldVersion uint32
	Chunks       []Chunk
}

// Chunk represents a single chunk in the slime world.
type Chunk struct {
	X            int32
	Z            int32
	Sections     []Section
	TileEntities []map[string]interface{}
	Entities     []map[string]interface{}
}

// Section represents a 16x16x16 chunk section.
type Section struct {
	BlockPalette []BlockState
	BlockStates  []int64 // packed block state indices
	BitsPerBlock int
}

// BlockState represents a block in the palette.
type BlockState struct {
	Name       string
	Properties map[string]string
}

// ReadSlimeWorld reads a slime world from raw bytes.
func ReadSlimeWorld(data []byte) (*SlimeWorld, error) {
	r := bytes.NewReader(data)
	world := &SlimeWorld{}

	// Read header
	var magic uint16
	if err := binary.Read(r, binary.BigEndian, &magic); err != nil {
		return nil, fmt.Errorf("reading magic: %w", err)
	}
	if magic != SlimeMagic {
		return nil, fmt.Errorf("invalid magic: 0x%04X (expected 0x%04X)", magic, SlimeMagic)
	}

	var version uint8
	if err := binary.Read(r, binary.BigEndian, &version); err != nil {
		return nil, fmt.Errorf("reading version: %w", err)
	}
	if version < SlimeVersionMin || version > SlimeVersionMax {
		return nil, fmt.Errorf("unsupported slime version: %d (supported: %d-%d)", version, SlimeVersionMin, SlimeVersionMax)
	}

	if err := binary.Read(r, binary.BigEndian, &world.WorldVersion); err != nil {
		return nil, fmt.Errorf("reading world version: %w", err)
	}

	var worldFlags uint8
	if version >= 0x0D { // v13+ added world flags
		if err := binary.Read(r, binary.BigEndian, &worldFlags); err != nil {
			return nil, fmt.Errorf("reading world flags: %w", err)
		}
	}

	// Read compressed chunks
	var compChunksSize, uncompChunksSize int32
	if err := binary.Read(r, binary.BigEndian, &compChunksSize); err != nil {
		return nil, fmt.Errorf("reading compressed chunks size: %w", err)
	}
	if err := binary.Read(r, binary.BigEndian, &uncompChunksSize); err != nil {
		return nil, fmt.Errorf("reading uncompressed chunks size: %w", err)
	}

	compChunksData := make([]byte, compChunksSize)
	if _, err := io.ReadFull(r, compChunksData); err != nil {
		return nil, fmt.Errorf("reading compressed chunks data: %w", err)
	}

	chunksData, err := decompressZstd(compChunksData)
	if err != nil {
		return nil, fmt.Errorf("decompressing chunks: %w", err)
	}
	if len(chunksData) != int(uncompChunksSize) {
		return nil, fmt.Errorf("chunk data size mismatch: got %d, expected %d", len(chunksData), uncompChunksSize)
	}

	// Parse chunks
	chunks, err := parseChunks(chunksData, worldFlags, version)
	if err != nil {
		return nil, fmt.Errorf("parsing chunks: %w", err)
	}
	world.Chunks = chunks

	// Read compressed extra data (skip it, not needed for schematic)
	var compExtraSize, uncompExtraSize int32
	if err := binary.Read(r, binary.BigEndian, &compExtraSize); err != nil {
		// May not exist if at end of file
		return world, nil
	}
	if err := binary.Read(r, binary.BigEndian, &uncompExtraSize); err != nil {
		return world, nil
	}
	// Skip the extra data
	if _, err := io.CopyN(io.Discard, r, int64(compExtraSize)); err != nil {
		return world, nil
	}

	return world, nil
}

func decompressZstd(data []byte) ([]byte, error) {
	decoder, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer decoder.Close()
	return io.ReadAll(decoder)
}

func parseChunks(data []byte, worldFlags uint8, version uint8) ([]Chunk, error) {
	r := bytes.NewReader(data)

	// Read chunk count (first 4 bytes of chunk data)
	var chunkCount int32
	if err := binary.Read(r, binary.BigEndian, &chunkCount); err != nil {
		return nil, fmt.Errorf("reading chunk count: %w", err)
	}

	chunks := make([]Chunk, 0, chunkCount)

	for i := int32(0); i < chunkCount; i++ {
		startPos := int(int64(len(data)) - int64(r.Len()))
		chunk, err := parseChunk(r, worldFlags, version)
		if err != nil {
			endPos := int(int64(len(data)) - int64(r.Len()))
			return nil, fmt.Errorf("chunk #%d/%d (x=%d z=%d, started at byte %d, failed at byte %d): %w",
				i, chunkCount, chunk.X, chunk.Z, startPos, endPos, err)
		}
		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

func parseChunk(r *bytes.Reader, worldFlags uint8, version uint8) (Chunk, error) {
	var chunk Chunk

	// Chunk coordinates
	if err := binary.Read(r, binary.BigEndian, &chunk.X); err != nil {
		return chunk, err
	}
	if err := binary.Read(r, binary.BigEndian, &chunk.Z); err != nil {
		return chunk, fmt.Errorf("reading chunk Z: %w", err)
	}

	// Section count
	var sectionCount int32
	if err := binary.Read(r, binary.BigEndian, &sectionCount); err != nil {
		return chunk, fmt.Errorf("reading section count: %w", err)
	}

	// Parse sections
	for i := int32(0); i < sectionCount; i++ {
		section, err := parseSection(r, version)
		if err != nil {
			return chunk, fmt.Errorf("parsing section %d: %w", i, err)
		}
		chunk.Sections = append(chunk.Sections, section)
	}

	// Read heightmaps (skip)
	if err := skipSizedData(r); err != nil {
		return chunk, fmt.Errorf("skipping heightmaps: %w", err)
	}

	// Handle additional flags
	// Order per doc: POI chunks, then block ticks, then fluid ticks

	// POI chunks (bitmask 1)
	if worldFlags&FlagPOIChunks != 0 {
		if err := skipSizedData(r); err != nil {
			return chunk, fmt.Errorf("skipping POI chunks: %w", err)
		}
	}

	// Block ticks (bitmask 4) - note: doc order puts this before fluid ticks
	if worldFlags&FlagBlockTicks != 0 {
		if err := skipSizedData(r); err != nil {
			return chunk, fmt.Errorf("skipping block ticks: %w", err)
		}
	}

	// Fluid ticks (bitmask 2)
	if worldFlags&FlagFluidTicks != 0 {
		if err := skipSizedData(r); err != nil {
			return chunk, fmt.Errorf("skipping fluid ticks: %w", err)
		}
	}

	// Tile entities
	tileEntities, err := readNBTListSection(r, "tileEntities")
	if err != nil {
		return chunk, fmt.Errorf("reading tile entities: %w", err)
	}
	chunk.TileEntities = tileEntities

	// Entities
	entities, err := readNBTListSection(r, "entities")
	if err != nil {
		return chunk, fmt.Errorf("reading entities: %w", err)
	}
	chunk.Entities = entities

	// Per-chunk extra data / PDC (size-prefixed, added in v12)
	if err := skipSizedData(r); err != nil {
		return chunk, fmt.Errorf("skipping chunk extra/PDC data: %w", err)
	}

	return chunk, nil
}

func parseSection(r *bytes.Reader, version uint8) (Section, error) {
	var section Section

	if version >= 0x0D {
		// v13+: single flags bitmask byte (1=blockLight, 2=skyLight)
		var flags uint8
		if err := binary.Read(r, binary.BigEndian, &flags); err != nil {
			return section, fmt.Errorf("reading section flags: %w", err)
		}

		// v13 order: skyLight first, then blockLight
		if flags&2 != 0 {
			if _, err := r.Seek(2048, io.SeekCurrent); err != nil {
				return section, fmt.Errorf("skipping sky light: %w", err)
			}
		}
		if flags&1 != 0 {
			if _, err := r.Seek(2048, io.SeekCurrent); err != nil {
				return section, fmt.Errorf("skipping block light: %w", err)
			}
		}
	} else {
		// v12: two separate booleans, interleaved with data
		// Order: blockLight boolean + data, then skyLight boolean + data
		var hasBlockLight uint8
		if err := binary.Read(r, binary.BigEndian, &hasBlockLight); err != nil {
			return section, fmt.Errorf("reading block light flag: %w", err)
		}
		if hasBlockLight != 0 {
			if _, err := r.Seek(2048, io.SeekCurrent); err != nil {
				return section, fmt.Errorf("skipping block light: %w", err)
			}
		}

		var hasSkyLight uint8
		if err := binary.Read(r, binary.BigEndian, &hasSkyLight); err != nil {
			return section, fmt.Errorf("reading sky light flag: %w", err)
		}
		if hasSkyLight != 0 {
			if _, err := r.Seek(2048, io.SeekCurrent); err != nil {
				return section, fmt.Errorf("skipping sky light: %w", err)
			}
		}
	}

	// Block states NBT
	var blockStatesSize int32
	if err := binary.Read(r, binary.BigEndian, &blockStatesSize); err != nil {
		return section, fmt.Errorf("reading block states size: %w", err)
	}

	if blockStatesSize > 0 {
		blockStatesData := make([]byte, blockStatesSize)
		if _, err := io.ReadFull(r, blockStatesData); err != nil {
			return section, fmt.Errorf("reading block states data: %w", err)
		}

		palette, states, bitsPerBlock, err := parseBlockStatesNBT(blockStatesData)
		if err != nil {
			return section, fmt.Errorf("parsing block states NBT: %w", err)
		}
		section.BlockPalette = palette
		section.BlockStates = states
		section.BitsPerBlock = bitsPerBlock
	}

	// Biomes NBT (skip)
	var biomesSize int32
	if err := binary.Read(r, binary.BigEndian, &biomesSize); err != nil {
		return section, fmt.Errorf("reading biomes size: %w", err)
	}
	if biomesSize > 0 {
		if _, err := r.Seek(int64(biomesSize), io.SeekCurrent); err != nil {
			return section, fmt.Errorf("skipping biomes: %w", err)
		}
	}

	return section, nil
}

// PaletteEntry is used for NBT deserialization of block state palette entries.
type PaletteEntry struct {
	Name       string            `nbt:"Name"`
	Properties map[string]string `nbt:"Properties"`
}

// BlockStatesNBT represents the Minecraft chunk section block_states compound.
type BlockStatesNBT struct {
	Palette []PaletteEntry `nbt:"palette"`
	Data    []int64        `nbt:"data"`
}

func parseBlockStatesNBT(data []byte) ([]BlockState, []int64, int, error) {
	var blockStates BlockStatesNBT
	if err := nbt.Unmarshal(data, &blockStates); err != nil {
		return nil, nil, 0, fmt.Errorf("unmarshalling block states: %w", err)
	}

	palette := make([]BlockState, len(blockStates.Palette))
	for i, entry := range blockStates.Palette {
		palette[i] = BlockState{
			Name:       entry.Name,
			Properties: entry.Properties,
		}
	}

	// Calculate bits per block
	bitsPerBlock := 4 // minimum
	paletteSize := len(palette)
	if paletteSize > 0 {
		bits := 0
		for (1 << bits) < paletteSize {
			bits++
		}
		if bits < 4 {
			bits = 4
		}
		bitsPerBlock = bits
	}

	return palette, blockStates.Data, bitsPerBlock, nil
}

func skipSizedData(r *bytes.Reader) error {
	var size int32
	if err := binary.Read(r, binary.BigEndian, &size); err != nil {
		return err
	}
	if size > 0 {
		if _, err := r.Seek(int64(size), io.SeekCurrent); err != nil {
			return err
		}
	}
	return nil
}

func readNBTListSection(r *bytes.Reader, listName string) ([]map[string]interface{}, error) {
	var size int32
	if err := binary.Read(r, binary.BigEndian, &size); err != nil {
		return nil, err
	}

	if size <= 0 {
		return nil, nil
	}

	nbtData := make([]byte, size)
	if _, err := io.ReadFull(r, nbtData); err != nil {
		return nil, err
	}

	// The NBT contains a compound with a list tag named listName
	var container map[string]interface{}
	if err := nbt.Unmarshal(nbtData, &container); err != nil {
		// If standard unmarshal fails, the data might be empty or malformed
		// Just skip it
		return nil, nil
	}

	listRaw, ok := container[listName]
	if !ok {
		return nil, nil
	}

	// The list should be a slice of interface{}
	listSlice, ok := listRaw.([]interface{})
	if !ok {
		return nil, nil
	}

	var result []map[string]interface{}
	for _, item := range listSlice {
		if m, ok := item.(map[string]interface{}); ok {
			result = append(result, m)
		}
	}

	return result, nil
}

// GetBlockAt returns the block state at a specific position within a section.
// x, y, z are local coordinates (0-15).
func (s *Section) GetBlockAt(x, y, z int) BlockState {
	if len(s.BlockPalette) == 0 {
		return BlockState{Name: "minecraft:air"}
	}
	if len(s.BlockPalette) == 1 {
		return s.BlockPalette[0]
	}
	if len(s.BlockStates) == 0 {
		return s.BlockPalette[0]
	}

	// Minecraft packed format: blocks are indexed as y*16*16 + z*16 + x
	blockIndex := y*256 + z*16 + x
	bitsPerBlock := s.BitsPerBlock

	// Number of blocks per long
	blocksPerLong := 64 / bitsPerBlock
	// Which long contains our block
	longIndex := blockIndex / blocksPerLong
	// Position within that long
	bitOffset := (blockIndex % blocksPerLong) * bitsPerBlock

	if longIndex >= len(s.BlockStates) {
		return s.BlockPalette[0]
	}

	mask := int64((1 << bitsPerBlock) - 1)
	paletteIndex := int((s.BlockStates[longIndex] >> bitOffset) & mask)

	if paletteIndex >= len(s.BlockPalette) {
		return s.BlockPalette[0]
	}

	return s.BlockPalette[paletteIndex]
}

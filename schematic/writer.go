package schematic

import (
	"bytes"
	"compress/gzip"
	"fmt"

	"github.com/Tnze/go-mc/nbt"
)

// Schematic represents a Sponge Schematic v3 (.schem) file.
type Schematic struct {
	Width       int
	Height      int
	Length      int
	DataVersion int32
	Offset      [3]int32

	// Palette maps block state strings to indices.
	// e.g. "minecraft:stone" -> 0, "minecraft:oak_planks" -> 1
	Palette map[string]int32

	// BlockData stores the palette index for each block position.
	// Indexed as: x + z*Width + y*Width*Length
	BlockData []int32

	BlockEntities []BlockEntity
	Entities      []Entity
}

// BlockEntity represents a block entity in the schematic.
type BlockEntity struct {
	Pos  [3]int32
	Id   string
	Data map[string]interface{}
}

// Entity represents an entity in the schematic.
type Entity struct {
	Pos  [3]float64
	Id   string
	Data map[string]interface{}
}

// NewSchematic creates a new empty schematic with the given dimensions.
func NewSchematic(width, height, length int, dataVersion int32) *Schematic {
	totalBlocks := width * height * length
	return &Schematic{
		Width:       width,
		Height:      height,
		Length:      length,
		DataVersion: dataVersion,
		Palette:     map[string]int32{"minecraft:air": 0},
		BlockData:   make([]int32, totalBlocks),
	}
}

// BlockStateString builds the palette key for a block state.
// e.g. "minecraft:oak_stairs[facing=north,half=bottom,shape=straight]"
func BlockStateString(name string, properties map[string]string) string {
	if len(properties) == 0 {
		return name
	}

	buf := make([]byte, 0, 64)
	buf = append(buf, name...)
	buf = append(buf, '[')
	first := true
	for k, v := range properties {
		if !first {
			buf = append(buf, ',')
		}
		buf = append(buf, k...)
		buf = append(buf, '=')
		buf = append(buf, v...)
		first = false
	}
	buf = append(buf, ']')
	return string(buf)
}

// SetBlock sets a block at the given coordinates using a block state string.
func (s *Schematic) SetBlock(x, y, z int, blockState string) {
	index := x + z*s.Width + y*s.Width*s.Length

	if index < 0 || index >= len(s.BlockData) {
		return
	}

	paletteIdx, ok := s.Palette[blockState]
	if !ok {
		paletteIdx = int32(len(s.Palette))
		s.Palette[blockState] = paletteIdx
	}

	s.BlockData[index] = paletteIdx
}

// Save writes the schematic to gzipped NBT bytes in Sponge Schematic v3 format.
func (s *Schematic) Save() ([]byte, error) {
	// Encode BlockData as varint byte array
	blockDataBytes := encodeVarIntArray(s.BlockData)

	// Build palette NBT: map[string]int32
	paletteNBT := make(map[string]int32, len(s.Palette))
	for k, v := range s.Palette {
		paletteNBT[k] = v
	}

	// Build Blocks container
	blocks := blocksContainerNBT{
		Palette: paletteNBT,
		Data:    blockDataBytes,
	}

	// Build block entities
	if len(s.BlockEntities) > 0 {
		beList := make([]blockEntityNBT, len(s.BlockEntities))
		for i, be := range s.BlockEntities {
			beList[i] = blockEntityNBT{
				Pos:  be.Pos,
				Id:   be.Id,
				Data: be.Data,
			}
		}
		blocks.BlockEntities = beList
	}

	// Build entities
	var entities []entityNBT
	if len(s.Entities) > 0 {
		entities = make([]entityNBT, len(s.Entities))
		for i, ent := range s.Entities {
			entities[i] = entityNBT{
				Pos:  ent.Pos,
				Id:   ent.Id,
				Data: ent.Data,
			}
		}
	}

	schemNBT := schemRootNBT{
		Version:     3,
		DataVersion: s.DataVersion,
		Width:       int16(s.Width),
		Height:      int16(s.Height),
		Length:      int16(s.Length),
		Offset:      s.Offset,
		Blocks:      blocks,
	}

	if len(entities) > 0 {
		schemNBT.Entities = entities
	}

	// Wrap in a root compound: {"": {"Schematic": {...}}}
	// WorldEdit's reader does: LinRootEntry.readFrom(stream).value().getTag("Schematic")
	// So the root tag must be a nameless compound containing a "Schematic" child.
	root := struct {
		Schematic schemRootNBT `nbt:"Schematic"`
	}{Schematic: schemNBT}

	// Encode to NBT
	var nbtBuf bytes.Buffer
	encoder := nbt.NewEncoder(&nbtBuf)
	if err := encoder.Encode(root, ""); err != nil {
		return nil, fmt.Errorf("encoding schematic NBT: %w", err)
	}

	// Compress with gzip
	var gzBuf bytes.Buffer
	gzWriter := gzip.NewWriter(&gzBuf)
	if _, err := gzWriter.Write(nbtBuf.Bytes()); err != nil {
		return nil, fmt.Errorf("compressing schematic: %w", err)
	}
	if err := gzWriter.Close(); err != nil {
		return nil, fmt.Errorf("closing gzip writer: %w", err)
	}

	return gzBuf.Bytes(), nil
}

// NBT serialization structures for Sponge Schematic v3

type schemRootNBT struct {
	Version     int32              `nbt:"Version"`
	DataVersion int32              `nbt:"DataVersion"`
	Width       int16              `nbt:"Width"`
	Height      int16              `nbt:"Height"`
	Length      int16              `nbt:"Length"`
	Offset      [3]int32           `nbt:"Offset"`
	Blocks      blocksContainerNBT `nbt:"Blocks"`
	Entities    []entityNBT        `nbt:"Entities,omitempty"`
}

type blocksContainerNBT struct {
	Palette       map[string]int32 `nbt:"Palette"`
	Data          []byte           `nbt:"Data"`
	BlockEntities []blockEntityNBT `nbt:"BlockEntities,omitempty"`
}

type blockEntityNBT struct {
	Pos  [3]int32               `nbt:"Pos"`
	Id   string                 `nbt:"Id"`
	Data map[string]interface{} `nbt:"Data,omitempty"`
}

type entityNBT struct {
	Pos  [3]float64             `nbt:"Pos"`
	Id   string                 `nbt:"Id"`
	Data map[string]interface{} `nbt:"Data,omitempty"`
}

// encodeVarIntArray encodes an array of int32s as a varint byte array.
func encodeVarIntArray(values []int32) []byte {
	buf := make([]byte, 0, len(values)) // at least 1 byte per value
	for _, v := range values {
		uv := uint32(v)
		for uv >= 0x80 {
			buf = append(buf, byte(uv&0x7F)|0x80)
			uv >>= 7
		}
		buf = append(buf, byte(uv))
	}
	return buf
}

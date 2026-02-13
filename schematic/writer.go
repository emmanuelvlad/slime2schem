package schematic

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"

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

	// blockData stores the palette index for each block position as uint16.
	// Supports up to 65535 unique block states (practical limit).
	// Indexed as: x + z*Width + y*Width*Length
	blockData []uint16

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
		blockData:   make([]uint16, totalBlocks),
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

	if index < 0 || index >= len(s.blockData) {
		return
	}

	paletteIdx, ok := s.Palette[blockState]
	if !ok {
		paletteIdx = int32(len(s.Palette))
		s.Palette[blockState] = paletteIdx
	}

	s.blockData[index] = uint16(paletteIdx)
}

// Save writes the schematic to gzipped NBT bytes in Sponge Schematic v3 format.
//
// NBT is written manually to avoid large intermediate allocations. The block
// data (often hundreds of MB as varints) is streamed directly from the uint16
// array into the gzip writer using a small buffer, so peak memory stays close
// to the size of blockData itself rather than 2-3x.
func (s *Schematic) Save() ([]byte, error) {
	var gzBuf bytes.Buffer
	gzWriter := gzip.NewWriter(&gzBuf)
	w := &nbtWriter{w: gzWriter}

	// Root compound (empty name — required by WorldEdit/FAWE)
	w.beginCompound("")

	// Schematic compound
	w.beginCompound("Schematic")
	w.writeInt("Version", 3)
	w.writeInt("DataVersion", s.DataVersion)
	w.writeShort("Width", int16(s.Width))
	w.writeShort("Height", int16(s.Height))
	w.writeShort("Length", int16(s.Length))
	w.writeIntArray("Offset", s.Offset[:])

	// Blocks compound
	w.beginCompound("Blocks")

	// Palette — each entry is an Int tag whose name is the block state
	w.beginCompound("Palette")
	for name, idx := range s.Palette {
		w.writeInt(name, idx)
	}
	w.endCompound()

	// Data — varint-encoded block data, streamed directly from blockData
	// This is the critical optimization: no intermediate []byte allocation.
	w.writeBlockDataVarints("Data", s.blockData)
	s.blockData = nil // release the 351MB array immediately

	// BlockEntities
	if len(s.BlockEntities) > 0 {
		w.writeNamedNBT(struct {
			BlockEntities []blockEntityNBT `nbt:"BlockEntities"`
		}{BlockEntities: toBlockEntityNBT(s.BlockEntities)})
	}

	w.endCompound() // Blocks

	// Entities
	if len(s.Entities) > 0 {
		w.writeNamedNBT(struct {
			Entities []entityNBT `nbt:"Entities"`
		}{Entities: toEntityNBT(s.Entities)})
	}

	w.endCompound() // Schematic
	w.endCompound() // root

	if w.err != nil {
		return nil, fmt.Errorf("encoding schematic NBT: %w", w.err)
	}

	if err := gzWriter.Close(); err != nil {
		return nil, fmt.Errorf("closing gzip writer: %w", err)
	}

	return gzBuf.Bytes(), nil
}

// ---------------------------------------------------------------------------
// Manual NBT writer — writes raw NBT tags to an io.Writer.
// Tracks the first error and skips subsequent writes on error.
// ---------------------------------------------------------------------------

type nbtWriter struct {
	w   io.Writer
	err error
}

// NBT tag type IDs
const (
	tagEnd       = 0
	tagByte      = 1
	tagShort     = 2
	tagInt       = 3
	tagFloat     = 5
	tagDouble    = 6
	tagByteArray = 7
	tagString    = 8
	tagList      = 9
	tagCompound  = 10
	tagIntArray  = 11
)

func (w *nbtWriter) write(data []byte) {
	if w.err != nil {
		return
	}
	_, w.err = w.w.Write(data)
}

func (w *nbtWriter) writeBE(v any) {
	if w.err != nil {
		return
	}
	w.err = binary.Write(w.w, binary.BigEndian, v)
}

func (w *nbtWriter) writeTagHeader(tagType byte, name string) {
	w.write([]byte{tagType})
	w.writeBE(uint16(len(name)))
	w.write([]byte(name))
}

func (w *nbtWriter) beginCompound(name string) {
	w.writeTagHeader(tagCompound, name)
}

func (w *nbtWriter) endCompound() {
	w.write([]byte{tagEnd})
}

func (w *nbtWriter) writeInt(name string, v int32) {
	w.writeTagHeader(tagInt, name)
	w.writeBE(v)
}

func (w *nbtWriter) writeShort(name string, v int16) {
	w.writeTagHeader(tagShort, name)
	w.writeBE(v)
}

func (w *nbtWriter) writeIntArray(name string, v []int32) {
	w.writeTagHeader(tagIntArray, name)
	w.writeBE(int32(len(v)))
	for _, val := range v {
		w.writeBE(val)
	}
}

// writeBlockDataVarints writes an NBT ByteArray tag whose content is the
// varint encoding of each uint16 in data. The varints are streamed through
// a small 4 KB buffer so no large intermediate slice is allocated.
func (w *nbtWriter) writeBlockDataVarints(name string, data []uint16) {
	if w.err != nil {
		return
	}

	// First pass: count the total varint byte length (no allocation).
	byteLen := int32(0)
	for _, v := range data {
		uv := uint32(v)
		for uv >= 0x80 {
			byteLen++
			uv >>= 7
		}
		byteLen++
	}

	// Write tag header + array length
	w.writeTagHeader(tagByteArray, name)
	w.writeBE(byteLen)

	// Second pass: encode varints through a small reusable buffer.
	buf := make([]byte, 0, 4096)
	for _, v := range data {
		uv := uint32(v)
		for uv >= 0x80 {
			buf = append(buf, byte(uv&0x7F)|0x80)
			uv >>= 7
		}
		buf = append(buf, byte(uv))

		if len(buf) >= 4000 {
			w.write(buf)
			buf = buf[:0]
			if w.err != nil {
				return
			}
		}
	}
	if len(buf) > 0 {
		w.write(buf)
	}
}

// writeNamedNBT encodes a struct's fields as NBT tags and injects them into
// the current compound. It uses go-mc/nbt for complex nested structures
// (entities, block entities with arbitrary Data maps), then strips the
// outer compound wrapper that nbt.Encode adds.
func (w *nbtWriter) writeNamedNBT(v any) {
	if w.err != nil {
		return
	}
	var buf bytes.Buffer
	if err := nbt.NewEncoder(&buf).Encode(v, ""); err != nil {
		w.err = err
		return
	}
	raw := buf.Bytes()
	// nbt.Encode wraps in: compound-type(1) + name-len(2) + name(0) + ... + end(1)
	// Strip the 3-byte header and 1-byte trailing End tag to get inner fields.
	if len(raw) < 4 {
		return
	}
	w.write(raw[3 : len(raw)-1])
}

// ---------------------------------------------------------------------------
// NBT helper structs — used only for entity serialization via go-mc/nbt
// ---------------------------------------------------------------------------

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

func toBlockEntityNBT(entities []BlockEntity) []blockEntityNBT {
	out := make([]blockEntityNBT, len(entities))
	for i, be := range entities {
		out[i] = blockEntityNBT{Pos: be.Pos, Id: be.Id, Data: be.Data}
	}
	return out
}

func toEntityNBT(entities []Entity) []entityNBT {
	out := make([]entityNBT, len(entities))
	for i, e := range entities {
		out[i] = entityNBT{Pos: e.Pos, Id: e.Id, Data: e.Data}
	}
	return out
}

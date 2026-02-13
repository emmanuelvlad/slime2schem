package converter

import (
	"fmt"
	"math"

	"github.com/emmanuelvlad/slime2schem/schematic"
	"github.com/emmanuelvlad/slime2schem/slime"
)

// ConvertResult contains the conversion output and statistics.
type ConvertResult struct {
	Schematic   *schematic.Schematic
	TotalBlocks int
}

// Convert transforms a slime world into a Sponge Schematic v3 (.schem).
func Convert(world *slime.SlimeWorld) (*ConvertResult, error) {
	if len(world.Chunks) == 0 {
		return nil, fmt.Errorf("no chunks in world")
	}

	// Determine world bounds
	minCX, minCZ := int32(math.MaxInt32), int32(math.MaxInt32)
	maxCX, maxCZ := int32(math.MinInt32), int32(math.MinInt32)
	minSY, maxSY := int32(math.MaxInt32), int32(math.MinInt32)

	for _, chunk := range world.Chunks {
		if chunk.X < minCX {
			minCX = chunk.X
		}
		if chunk.X > maxCX {
			maxCX = chunk.X
		}
		if chunk.Z < minCZ {
			minCZ = chunk.Z
		}
		if chunk.Z > maxCZ {
			maxCZ = chunk.Z
		}

		for sIdx := range chunk.Sections {
			sectionY := int32(sIdx)
			if sectionY < minSY {
				minSY = sectionY
			}
			if sectionY > maxSY {
				maxSY = sectionY
			}
		}
	}

	if minSY > maxSY {
		minSY = 0
		maxSY = 0
	}

	// Calculate dimensions
	chunksX := int(maxCX - minCX + 1)
	chunksZ := int(maxCZ - minCZ + 1)
	sectionsY := int(maxSY - minSY + 1)

	width := chunksX * 16    // X axis
	length := chunksZ * 16   // Z axis
	height := sectionsY * 16 // Y axis

	if width > 65535 || height > 65535 || length > 65535 {
		return nil, fmt.Errorf("world too large for schematic: %dx%dx%d", width, height, length)
	}

	fmt.Printf("World bounds: chunks X=[%d, %d] Z=[%d, %d] sections Y=[%d, %d]\n",
		minCX, maxCX, minCZ, maxCZ, minSY, maxSY)
	fmt.Printf("Schematic dimensions: %d x %d x %d (W x H x L)\n", width, height, length)

	schem := schematic.NewSchematic(width, height, length, int32(world.WorldVersion))

	// Set offset so that pasting centers the schematic on the player's X/Z position,
	// with the bottom of the schematic at the player's Y position.
	schem.Offset = [3]int32{-int32(width / 2), 0, -int32(length / 2)}

	totalBlocks := 0

	// Fill in blocks
	for _, chunk := range world.Chunks {
		// Chunk position relative to the schematic origin
		baseX := int(chunk.X-minCX) * 16
		baseZ := int(chunk.Z-minCZ) * 16

		for sIdx, section := range chunk.Sections {
			sectionY := int32(sIdx)
			baseY := int(sectionY-minSY) * 16

			for y := 0; y < 16; y++ {
				for z := 0; z < 16; z++ {
					for x := 0; x < 16; x++ {
						bs := section.GetBlockAt(x, y, z)

						if bs.Name == "minecraft:air" || bs.Name == "minecraft:cave_air" || bs.Name == "minecraft:void_air" {
							continue
						}

						sx := baseX + x
						sy := baseY + y
						sz := baseZ + z

						if sx >= 0 && sx < width && sy >= 0 && sy < height && sz >= 0 && sz < length {
							blockState := schematic.BlockStateString(bs.Name, bs.Properties)
							schem.SetBlock(sx, sy, sz, blockState)
							totalBlocks++
						}
					}
				}
			}
		}

		// Add block entities with adjusted coordinates
		for _, te := range chunk.TileEntities {
			be := adjustBlockEntity(te, int(minCX)*16, int(minSY)*16, int(minCZ)*16)
			if be != nil {
				schem.BlockEntities = append(schem.BlockEntities, *be)
			}
		}

		// Add entities with adjusted coordinates (only if within schematic bounds)
		for _, ent := range chunk.Entities {
			e := adjustEntity(ent, int(minCX)*16, int(minSY)*16, int(minCZ)*16)
			if e != nil &&
				e.Pos[0] >= 0 && e.Pos[0] < float64(width) &&
				e.Pos[1] >= 0 && e.Pos[1] < float64(height) &&
				e.Pos[2] >= 0 && e.Pos[2] < float64(length) {
				schem.Entities = append(schem.Entities, *e)
			}
		}
	}

	return &ConvertResult{
		Schematic:   schem,
		TotalBlocks: totalBlocks,
	}, nil
}

// adjustBlockEntity converts a raw tile entity map to a schematic BlockEntity
// with coordinates relative to the schematic origin.
func adjustBlockEntity(te map[string]interface{}, offsetX, offsetY, offsetZ int) *schematic.BlockEntity {
	id, _ := te["id"].(string)
	if id == "" {
		// Try capitalized variant
		id, _ = te["Id"].(string)
	}
	if id == "" {
		return nil
	}

	x, xOk := getInt(te, "x")
	y, yOk := getInt(te, "y")
	z, zOk := getInt(te, "z")
	if !xOk || !yOk || !zOk {
		return nil
	}

	// Build extra data (everything except id, x, y, z which are handled separately)
	data := make(map[string]interface{}, len(te))
	for k, v := range te {
		switch k {
		case "id", "Id", "x", "y", "z":
			continue
		default:
			data[k] = v
		}
	}

	return &schematic.BlockEntity{
		Pos:  [3]int32{int32(x - offsetX), int32(y - offsetY), int32(z - offsetZ)},
		Id:   id,
		Data: data,
	}
}

// adjustEntity converts a raw entity map to a schematic Entity
// with coordinates relative to the schematic origin.
func adjustEntity(ent map[string]interface{}, offsetX, offsetY, offsetZ int) *schematic.Entity {
	id, _ := ent["id"].(string)
	if id == "" {
		id, _ = ent["Id"].(string)
	}
	if id == "" {
		return nil
	}

	// Entity position is stored in Pos as a double list [x, y, z]
	pos, ok := ent["Pos"].([]interface{})
	if !ok || len(pos) < 3 {
		return nil
	}

	px, pxOk := toFloat64(pos[0])
	py, pyOk := toFloat64(pos[1])
	pz, pzOk := toFloat64(pos[2])
	if !pxOk || !pyOk || !pzOk {
		return nil
	}

	// Build extra data (everything except id and Pos)
	data := make(map[string]interface{}, len(ent))
	for k, v := range ent {
		switch k {
		case "id", "Id", "Pos":
			continue
		default:
			data[k] = v
		}
	}

	return &schematic.Entity{
		Pos:  [3]float64{px - float64(offsetX), py - float64(offsetY), pz - float64(offsetZ)},
		Id:   id,
		Data: data,
	}
}

func getInt(m map[string]interface{}, key string) (int, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch val := v.(type) {
	case int32:
		return int(val), true
	case int64:
		return int(val), true
	case int:
		return val, true
	case float64:
		return int(val), true
	default:
		return 0, false
	}
}

func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int32:
		return float64(val), true
	case int64:
		return float64(val), true
	default:
		return 0, false
	}
}

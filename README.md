# slime2schem

Converts [SlimeWorld](https://github.com/InfernalSuite/AdvancedSlimePaper) (`.slime`) files to [Sponge Schematic v3](https://github.com/SpongePowered/Schematic-Specification) (`.schem`) format.

The output schematic can be loaded in Minecraft using [WorldEdit](https://enginehub.org/worldedit) or [FastAsyncWorldEdit](https://github.com/IntellectualSites/FastAsyncWorldEdit).

## Features

- Parses SlimeWorld format v13 (AdvancedSlimePaper)
- Outputs Sponge Schematic v3 (`.schem`)
- Preserves block states with full property data (no legacy ID mapping)
- Preserves block entity data (chests, shulker boxes, campfires, decorated pots, etc.)
- Preserves entity data (item displays, interactions, mobs, etc.)
- Schematic is centered on the paste point (X/Z)

## Installation

```sh
go install github.com/emmanuelvlad/slime2schem@latest
```

Or build from source:

```sh
git clone https://github.com/emmanuelvlad/slime2schem.git
cd slime2schem
go build -o slime2schem .
```

## Usage

### Command Line

```sh
slime2schem <world.slime>
```

This produces `world.schem` in the same directory.

You can also specify the output path:

```sh
slime2schem -input world.slime -output my_build.schem
```

### Programmatic Usage

```go
package main

import (
	"os"

	"github.com/emmanuelvlad/slime2schem/converter"
	"github.com/emmanuelvlad/slime2schem/slime"
)

func main() {
	// Read the .slime file
	worldData, err := os.ReadFile("world.slime")
	if err != nil {
		panic(err)
	}

	// Parse the SlimeWorld
	slimeWorld, err := slime.ReadSlimeWorld(worldData)
	if err != nil {
		panic(err)
	}

	// Convert to schematic
	converted, err := converter.Convert(slimeWorld)
	if err != nil {
		panic(err)
	}

	// Save as .schem
	schemData, err := converted.Schematic.Save()
	if err != nil {
		panic(err)
	}

	// Write to file
	err = os.WriteFile("world.schem", schemData, 0644)
	if err != nil {
		panic(err)
	}
}
```

### Loading in Minecraft

1. Place the `.schem` file in your WorldEdit schematics folder
2. In-game, run:
   ```
   //schematic load <filename>
   //paste
   ```

## Memory Usage

The converter allocates a flat array covering the entire bounding box of the world. Memory usage is determined by the **schematic volume**, not the slime file size:

```
memory ≈ width × height × length × 2 bytes
```

Where dimensions are derived from the chunk and section bounding box:
- **Width** = (maxChunkX − minChunkX + 1) × 16
- **Length** = (maxChunkZ − minChunkZ + 1) × 16
- **Height** = (maxSectionY − minSectionY + 1) × 16

For example, a 2.4 MB slime world spanning 39×48 chunks with sections 0–23 produces a 624×384×768 schematic (184M blocks), requiring **~350 MB** of peak memory.

A compact world (e.g. 10×10 chunks, 4 sections tall) would use only ~20 MB regardless of slime file size.

> [!CAUTION]
> Worlds with sparse vertical content (e.g. a few blocks at Y=0 and Y=368) will force a large height span and high memory usage. Consider trimming extreme sections before conversion if memory is constrained.

## How it works

1. **Parse** — Reads the `.slime` binary format (zstd-compressed chunk data, sections, block palettes, tile entities, entities)
2. **Convert** — Maps all chunks into a single schematic volume, translating block states, block entities, and entities to schematic-relative coordinates
3. **Write** — Encodes the result as gzipped NBT in Sponge Schematic v3 format with varint-compressed block data

## License

MIT

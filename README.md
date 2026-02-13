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

```sh
slime2schem <world.slime>
```

This produces `world.schem` in the same directory.

You can also specify the output path:

```sh
slime2schem -input world.slime -output my_build.schem
```

### Loading in Minecraft

1. Place the `.schem` file in your WorldEdit schematics folder
2. In-game, run:
   ```
   //schematic load <filename>
   //paste
   ```

## How it works

1. **Parse** — Reads the `.slime` binary format (zstd-compressed chunk data, sections, block palettes, tile entities, entities)
2. **Convert** — Maps all chunks into a single schematic volume, translating block states, block entities, and entities to schematic-relative coordinates
3. **Write** — Encodes the result as gzipped NBT in Sponge Schematic v3 format with varint-compressed block data

## License

MIT

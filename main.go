package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/emmanuelvlad/slime2schem/converter"
	"github.com/emmanuelvlad/slime2schem/slime"
)

func main() {
	inputFile := flag.String("input", "", "Path to the .slime file to convert")
	outputFile := flag.String("output", "", "Path for the output .schem file (default: input name with .schem extension)")
	flag.Parse()

	// Allow positional argument as input
	if *inputFile == "" && flag.NArg() > 0 {
		*inputFile = flag.Arg(0)
	}

	if *inputFile == "" {
		fmt.Fprintf(os.Stderr, "Usage: slime2schem [-input] <file.slime> [-output file.schem]\n")
		fmt.Fprintf(os.Stderr, "\nConverts a SlimeWorld (.slime) file to Sponge Schematic v3 (.schem) format.\n")
		fmt.Fprintf(os.Stderr, "The output schematic can be pasted in Minecraft using WorldEdit.\n\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if *outputFile == "" {
		ext := filepath.Ext(*inputFile)
		base := strings.TrimSuffix(*inputFile, ext)
		*outputFile = base + ".schem"
	}

	fmt.Printf("Reading slime world: %s\n", *inputFile)

	data, err := os.ReadFile(*inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input file: %v\n", err)
		os.Exit(1)
	}

	world, err := slime.ReadSlimeWorld(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing slime world: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Parsed %d chunks (data version: %d)\n", len(world.Chunks), world.WorldVersion)

	result, err := converter.Convert(world)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error converting: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Converted %d non-air blocks (%d unique block states)\n",
		result.TotalBlocks, len(result.Schematic.Palette))

	schemData, err := result.Schematic.Save()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error saving schematic: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(*outputFile, schemData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing output file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Schematic saved to: %s\n", *outputFile)
	fmt.Printf("Dimensions: %d x %d x %d (Width x Height x Length)\n",
		result.Schematic.Width, result.Schematic.Height, result.Schematic.Length)
	fmt.Println("\nYou can load this schematic in Minecraft using WorldEdit:")
	fmt.Println("  //schematic load <filename>")
	fmt.Println("  //paste")
}

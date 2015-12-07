package app

import (
	// "yap/nlp/format/lattice"

	// nlp "yap/nlp/types"
	"yap/nlp/parser/ma"
	// "yap/util"

	// "fmt"
	"log"
	// "os"

	"github.com/gonuts/commander"
	"github.com/gonuts/flag"
)

var (
	latFile, rawFile, conlluFile, dataFile string
	useConllU                              bool // TODO: whatever i don't care anymore
)

func MALearnConfigOut() {
	log.Println("Configuration")
	if useConllU {
		log.Printf("CoNLL-U:\t%s", conlluFile)
	} else {
		log.Printf("Lattice:\t%s", latFile)
		log.Printf("Raw:    \t%s", rawFile)
	}
	log.Println()
	log.Printf("Output:    \t%s", dataFile)
	log.Println()
}

func MALearn(cmd *commander.Command, args []string) {
	var REQUIRED_FLAGS []string
	useConllU = len(conlluFile) > 0
	if useConllU {
		useConllU = true
		REQUIRED_FLAGS = []string{"conllu", "out"}
	} else {
		REQUIRED_FLAGS = []string{"lattice", "raw", "out"}
	}

	VerifyFlags(cmd, REQUIRED_FLAGS)

	MALearnConfigOut()
	log.Println("Starting learning for data-driven morphological analyzer")
	maData := new(ma.MADict)
	maData.Language = "Test"
	var (
		numLearned int
		err        error
	)
	if useConllU {
		numLearned, err = maData.LearnFromConllU(conlluFile)
	} else {
		numLearned, err = maData.LearnFromLat(latFile, rawFile)
	}
	if err != nil {
		log.Println("Got error learning", err)
		return
	}
	log.Println("Learned", numLearned, "new tokens")
	maData.WriteFile(dataFile)
}

func MALearnCmd() *commander.Command {
	cmd := &commander.Command{
		Run:       MALearn,
		UsageLine: "malearn <file options> [arguments]",
		Short:     "generate a data-driven morphological analysis dictionary for a set of files",
		Long: `
generate a data-driven morphological analysis dictionary for a set of files

	$ ./yap malearn -lattice <lattice file> -raw <raw file> [options]

`,
		Flag: *flag.NewFlagSet("malearn", flag.ExitOnError),
	}
	cmd.Flag.StringVar(&latFile, "lattice", "", "Lattice-format input file")
	cmd.Flag.StringVar(&rawFile, "raw", "", "raw sentences input file")
	cmd.Flag.StringVar(&conlluFile, "conllu", "", "CoNLL-U-format input file")
	cmd.Flag.StringVar(&dataFile, "out", "", "output file")
	return cmd
}

package app

import (
	"yap/nlp/format/conllu"
	"yap/nlp/format/lattice"
	"yap/nlp/format/lex"
	"yap/nlp/format/raw"
	"yap/util"

	"yap/nlp/parser/ma"
	"yap/nlp/parser/xliter8"
	nlp "yap/nlp/types"
	// "yap/util"

	"fmt"
	"log"
	// "os"

	"github.com/gonuts/commander"
	"github.com/gonuts/flag"
)

var (
	prefixFile, lexiconFile string
	xliter8out, alwaysnnp   bool
	nnpnofeats              bool
	showoov                 bool
	outJSON                 bool
	DEFAULT_DATA_DIRS       = []string{".", "data/bgulex"}
)

func HebMAConfigOut() {
	log.Println("Configuration")
	log.Printf("Heb Lexicon:\t\t%s", prefixFile)
	log.Printf("Heb Prefix:\t\t%s", lexiconFile)
	log.Printf("OOV Strategy:\t%v", "Const:NNP")
	log.Printf("xliter8 out:\t\t%v", xliter8out)
	log.Println()
	if useConllU {
		log.Printf("CoNLL-U Input:\t%s", conlluFile)
	} else {
		log.Printf("Raw Input:\t\t%s", inRawFile)
	}
	log.Printf("Output:\t\t%s", outLatticeFile)
	log.Println()
}

func HebMA(cmd *commander.Command, args []string) error {
	useConllU = len(conlluFile) > 0
	var REQUIRED_FLAGS []string
	if useConllU {
		lattice.OVERRIDE_XPOS_WITH_UPOS = true
		REQUIRED_FLAGS = []string{"conllu", "out"}
	} else {
		REQUIRED_FLAGS = []string{"raw", "out"}
	}
	prefixLocation, found := util.LocateFile(prefixFile, DEFAULT_DATA_DIRS)
	if found {
		prefixFile = prefixLocation
	} else {
		REQUIRED_FLAGS = append(REQUIRED_FLAGS, "prefix")
	}
	lexiconLocation, found := util.LocateFile(lexiconFile, DEFAULT_DATA_DIRS)
	if found {
		lexiconFile = lexiconLocation
	} else {
		REQUIRED_FLAGS = append(REQUIRED_FLAGS, "lexicon")
	}
	VerifyFlags(cmd, REQUIRED_FLAGS)
	HebMAConfigOut()
	if outFormat == "ud" {
		// override all skips in HEBLEX
		lex.SKIP_POLAR = false
		lex.SKIP_BINYAN = false
		lex.SKIP_ALL_TYPE = false
		lex.SKIP_TYPES = make(map[string]bool)
		lattice.IGNORE_LEMMA = false
		// Compatibility: No features for PROPN in UD Hebrew
		lex.STRIP_ALL_NNP_OF_FEATS = true
	}
	maData := new(ma.BGULex)
	maData.MAType = outFormat
	log.Println("Reading Morphological Analyzer BGU Prefixes")
	maData.LoadPrefixes(prefixFile)
	log.Println("Reading Morphological Analyzer BGU Lexicon")
	maData.LoadLex(lexiconFile, nnpnofeats)
	log.Println()
	var (
		sents        []nlp.BasicSentence
		sentComments [][]string
		sentsStream  chan nlp.BasicSentence
		err          error
	)
	if Stream {
		if useConllU {
			log.Println("Piping conllu file to analyzer", conlluFile)
			conllStream, err := conllu.ReadFileAsStream(conlluFile, limit)
			if err != nil {
				panic(fmt.Sprintf("Failed reading CoNLL-U file - %v", err))
			}
			sentsStream = make(chan nlp.BasicSentence, 2)
			go func() {
				var i int
				for sent := range conllStream {
					newSent := make([]nlp.Token, len(sent.Tokens))
					for j, token := range sent.Tokens {
						newSent[j] = nlp.Token(token)
					}
					i++
					sentsStream <- newSent
				}
				close(sentsStream)
			}()

		} else {
			log.Println("Piping raw file to analyzer", inRawFile)
			sentsStream, err = raw.ReadFileAsStream(inRawFile, limit)
			if err != nil {
				panic(fmt.Sprintf("Failed reading raw file - %v", err))
			}
		}
	} else {
		if useConllU {
			conllSents, _, err := conllu.ReadFile(conlluFile, limit)
			if err != nil {
				panic(fmt.Sprintf("Failed reading CoNLL-U file - %v", err))
			}
			sents = make([]nlp.BasicSentence, len(conllSents))
			sentComments = make([][]string, len(conllSents))
			for i, sent := range conllSents {
				newSent := make([]nlp.Token, len(sent.Tokens))
				for j, token := range sent.Tokens {
					newSent[j] = nlp.Token(token)
				}
				sentComments[i] = sent.Comments
				sents[i] = newSent
			}
		} else {
			sents, err = raw.ReadFile(inRawFile, limit)
			if err != nil {
				panic(fmt.Sprintf("Failed reading raw file - %v", err))
			}
		}
	}
	log.Println("Running Hebrew Morphological Analysis")
	stats := new(ma.AnalyzeStats)
	stats.Init()
	maData.Stats = stats
	maData.AlwaysNNP = alwaysnnp
	maData.LogOOV = showoov
	prefix := log.Prefix()
	if Stream {
		lattices := make(chan nlp.LatticeSentence, 2)
		oovInd := make([]interface{}, 0, 100000)
		go func() {
			var i int
			for sent := range sentsStream {
				// log.SetPrefix(fmt.Sprintf("%v graph# %v ", prefix, i))
				lattice, ind := maData.Analyze(sent.Tokens())
				oovInd = append(oovInd, ind)
				if i%100 == 0 {
					log.Println("At sent", i)
				}
				lattices <- lattice
				i++
			}
			close(lattices)
		}()

		var hebrew xliter8.Interface
		if xliter8out {
			hebrew = &xliter8.Hebrew{}
		}
		output := lattice.Sentence2LatticeStream(lattices, hebrew)
		lattice.WriteStreamToFile(outLatticeFile, output)
		if oovFile != "" {
			raw.WriteFile(oovFile, oovInd)
		}
	} else {

		lattices := make([]nlp.LatticeSentence, len(sents))
		oovInd := make([]interface{}, len(sents))
		for i, sent := range sents {
			log.SetPrefix(fmt.Sprintf("%v graph# %v ", prefix, i))
			lattices[i], oovInd[i] = maData.Analyze(sent.Tokens())
		}
		var hebrew xliter8.Interface
		if xliter8out {
			hebrew = &xliter8.Hebrew{}
		}
		output := lattice.Sentence2LatticeCorpus(lattices, hebrew)
		if outFormat == "ud" {
			if outJSON {
				lattice.WriteUDJSONFile(outLatticeFile, output)
			} else {
				oovAsBasicArray := make([]nlp.BasicSentence, len(sents))
				for i, value := range oovInd {
					oovAsBasicArray[i] = value.(nlp.BasicSentence)
				}
				lattice.WriteUDFile(outLatticeFile, output, sentComments, oovAsBasicArray)
			}
		} else if outFormat == "spmrl" {
			lattice.WriteFile(outLatticeFile, output)
		} else {
			panic(fmt.Sprintf("Unknown lattice output format - %v", outFormat))
		}
		if oovFile != "" {
			raw.WriteFile(oovFile, oovInd)
		}
	}
	log.SetPrefix(prefix)
	log.Println("Analyzed", stats.TotalTokens, "occurences of", len(stats.UniqTokens), "unique tokens")
	log.Println("Encountered", stats.OOVTokens, "occurences of", len(stats.UniqOOVTokens), "unknown tokens")
	return nil
}

func HebMACmd() *commander.Command {
	cmd := &commander.Command{
		Run:       HebMA,
		UsageLine: "hebma <file options> [arguments]",
		Short:     "run lexicon-based morphological analyzer on raw input",
		Long: `
run lexicon-based morphological analyzer on raw input

	$ ./yap hebma -prefix <prefix file> -lexicon <lexicon file> -raw <raw file> -out <output file> [options]

`,
		Flag: *flag.NewFlagSet("ma", flag.ExitOnError),
	}
	cmd.Flag.StringVar(&prefixFile, "prefix", "bgupreflex_withdef.utf8.hr", "Prefix file for morphological analyzer")
	cmd.Flag.StringVar(&lexiconFile, "lexicon", "bgulex.utf8.hr", "Lexicon file for morphological analyzer")
	cmd.Flag.StringVar(&inRawFile, "raw", "", "Input raw (tokenized) file")
	cmd.Flag.StringVar(&conlluFile, "conllu", "", "CoNLL-U-format input file")
	cmd.Flag.StringVar(&outLatticeFile, "out", "", "Output lattice file")
	cmd.Flag.BoolVar(&xliter8out, "xliter8out", false, "Transliterate output lattice file")
	cmd.Flag.BoolVar(&alwaysnnp, "alwaysnnp", false, "Always add NNP to tokens and prefixed subtokens")
	cmd.Flag.BoolVar(&nnpnofeats, "addnnpnofeats", false, "Add NNP in lex but without features")
	cmd.Flag.IntVar(&limit, "limit", 0, "Limit input set")
	cmd.Flag.BoolVar(&showoov, "showoov", false, "Output OOV tokens")
	cmd.Flag.StringVar(&oovFile, "oov", "", "Output OOV File")
	cmd.Flag.BoolVar(&lex.LOG_FAILURES, "showlexerror", false, "Log errors encountered when loading the lexicon")
	cmd.Flag.StringVar(&outFormat, "format", "spmrl", "Output lattice format [spmrl|ud]")
	cmd.Flag.BoolVar(&outJSON, "json", false, "Output using JSON")
	cmd.Flag.BoolVar(&Stream, "stream", false, "Stream data from input through parser to output")
	return cmd
}

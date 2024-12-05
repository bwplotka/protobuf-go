// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The protoc-gen-go binary is a protoc plugin to generate Go code for
// both proto2 and proto3 versions of the protocol buffer language.
//
// For more information about the usage of this plugin, see:
// https://protobuf.dev/reference/go/go-generated.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	gengo "google.golang.org/protobuf/cmd/protoc-gen-go/internal_gengo"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/internal/version"
	"google.golang.org/protobuf/types/gofeaturespb"
)

const genGoDocURL = "https://protobuf.dev/reference/go/go-generated"
const grpcDocURL = "https://grpc.io/docs/languages/go/quickstart/#regenerate-grpc-code"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Fprintf(os.Stdout, "%v %v\n", filepath.Base(os.Args[0]), version.String())
		os.Exit(0)
	}
	if len(os.Args) == 2 && os.Args[1] == "--help" {
		fmt.Fprintf(os.Stdout, "See "+genGoDocURL+" for usage information.\n")
		os.Exit(0)
	}

	const defaultAPILevel = gofeaturespb.GoFeatures_API_OPEN

	var (
		flags                                 flag.FlagSet
		plugins                               = flags.String("plugins", "", "deprecated option")
		experimentalStripNonFunctionalCodegen = flags.Bool("experimental_strip_nonfunctional_codegen", false, "experimental_strip_nonfunctional_codegen true means that the plugin will not emit certain parts of the generated code in order to make it possible to compare a proto2/proto3 file with its equivalent (according to proto spec) editions file. Primarily, this is the encoded descriptor.")
		// TODO(bwplotka): Probably there should be a generic per message/file option switching.
		experimentalDefaultAPILevel = flags.String("experimental_default_api_level", gofeaturespb.GoFeatures_APILevel_name[int32(defaultAPILevel)], "What API to use for generation by default. Available options: API_OPEN, API_HYBRID, API_OPAQUE.")
	)
	protogen.Options{
		ParamFunc:                    flags.Set,
		InternalStripForEditionsDiff: experimentalStripNonFunctionalCodegen,
		APILevel: func() gofeaturespb.GoFeatures_APILevel {
			a, ok := gofeaturespb.GoFeatures_APILevel_value[*experimentalDefaultAPILevel]
			if !ok || a == int32(gofeaturespb.GoFeatures_API_LEVEL_UNSPECIFIED) {
				return defaultAPILevel
			}
			return gofeaturespb.GoFeatures_APILevel(a)
		},
	}.Run(func(gen *protogen.Plugin) error {
		if *plugins != "" {
			return errors.New("protoc-gen-go: plugins are not supported; use 'protoc --go-grpc_out=...' to generate gRPC\n\n" +
					"See " + grpcDocURL + " for more information.")
		}
		for _, f := range gen.Files {
			if f.Generate {
				gengo.GenerateFile(gen, f)
			}
		}
		gen.SupportedFeatures = gengo.SupportedFeatures
		gen.SupportedEditionsMinimum = gengo.SupportedEditionsMinimum
		gen.SupportedEditionsMaximum = gengo.SupportedEditionsMaximum
		return nil
	})
}

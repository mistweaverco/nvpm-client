#!/usr/bin/env bash

go run -ldflags "-X 'github.com/mistweaverco/nvpm-client/cmd/nvpm.VERSION=${VERSION}'" main.go

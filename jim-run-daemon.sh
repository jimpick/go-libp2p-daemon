#! /bin/bash

cd p2pd
#GOLOG_LOG_LEVEL=debug go run . -quic=false
#go run . -quic=false
go run . -hostAddrs /ip4/127.0.0.1/tcp/2071

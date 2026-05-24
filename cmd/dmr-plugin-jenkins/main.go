// dmr-plugin-jenkins is a DMR external plugin for multi-instance Jenkins (REST API via net/http).
package main

import (
	goplugin "github.com/hashicorp/go-plugin"
	"github.com/seanly/dmr-plugin-jenkins/internal/jenkins"
	"github.com/seanly/dmr/pkg/plugin/proto"
)

func main() {
	impl := jenkins.NewJenkinsPlugin()

	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: proto.Handshake,
		Plugins: map[string]goplugin.Plugin{
			"dmr-plugin": &proto.DMRPlugin{Impl: impl},
		},
	})
}

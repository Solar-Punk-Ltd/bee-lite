// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build android

package net

import (
	"os/exec"
	"strings"
	"time"
)

var (
	defaultAndroidNS = []string{"8.8.8.8:53", "8.8.4.4:53", "1.1.1.1:53"}
)

func dnsReadConfig(filename string) *dnsConfig {
	conf := &dnsConfig{
		ndots:    1,
		timeout:  5 * time.Second,
		attempts: 2,
		rotate:   false,
	}

	for _, prop := range []string{"net.dns1", "net.dns2"} {
		out, err := exec.Command("/system/bin/getprop", prop).Output()
		if err != nil {
			continue
		}
		ip := strings.TrimSpace(string(out))
		if ParseIP(ip) != nil {
			conf.servers = append(conf.servers, JoinHostPort(ip, "53"))
		}
	}

	if len(conf.servers) == 0 {
		conf.servers = defaultAndroidNS
	}

	return conf
}

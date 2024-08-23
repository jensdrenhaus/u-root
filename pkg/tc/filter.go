// Copyright 2012-20124 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package trafficctl

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/florianl/go-tc"
	"github.com/florianl/go-tc/core"
)

var (
	ErrInvalidFilterType = errors.New("invalid filtertype")
)

// FArgs hold all possible args for qdisc subcommand
// tc filter [ add | del | change | replace | show ] [ dev STRING ]
// tc filter [ add | del | change | replace | show ] [ block BLOCK_INDEX ]
// tc filter get dev STRING parent CLASSID protocol PROTO handle FILTERID pref PRIO FILTER_TYPE
// tc filter get block BLOCK_INDEX protocol PROTO handle FILTERID pref PRIO FILTER_TYPE
// [ pref PRIO ] protocol PROTO [ chain CHAIN_INDEX ]
// [ estimator INTERVAL TIME_CONSTANT ]
// [ root | ingress | egress | parent CLASSID ]
// [ handle FILTERID ] [ [ FILTER_TYPE ] [ help | OPTIONS ] ]
// tc filter show [ dev STRING ] [ root | ingress | egress | parent CLASSID ]
// tc filter show [ block BLOCK_INDEX ]
type FArgs struct {
	dev       string
	kind      *string
	parent    *uint32
	handle    *uint32
	protocol  *uint32
	pref      *uint32
	filterObj *tc.Object
}

func ParseFilterArgs(stdout io.Writer, args []string) (*FArgs, error) {
	pref := uint32(0)
	proto := uint32(0)
	ret := &FArgs{
		pref:     &pref,
		protocol: &proto,
	}
	if len(args) < 1 {
		return nil, ErrNotEnoughArgs
	}

	for i := 0; i < len(args); i = i + 2 {
		var val string
		if len(args[1:]) > i {
			val = args[i+1]
		}

		switch args[i] {
		case "dev":
			ret.dev = val
		case "parent":
			parent, err := ParseClassID(args[i+1])
			if err != nil {
				return nil, err
			}
			indirect := uint32(parent)
			ret.parent = &indirect
		case "protocol", "proto":
			proto, err := ParseProto(args[i+1])
			if err != nil {
				return nil, err
			}
			ret.protocol = &proto
		case "handle":
			maj, _, ok := strings.Cut(args[i+1], ":")
			if !ok {
				return nil, ErrInvalidArg
			}

			major, err := strconv.ParseUint(maj, 16, 16)
			if err != nil {
				return nil, err
			}

			indirect := uint32(major)
			ret.handle = &indirect
		case "preference", "pref", "priority":
			val, err := strconv.Atoi(val)
			if err != nil {
				return nil, err
			}
			if val < 0 || val >= 0x7FFFFFFF {
				return nil, ErrOutOfBounds
			}
			indirect := uint32(val)
			ret.pref = &indirect
		case "block":
			return nil, ErrNotImplemented
		case "chain":
			return nil, ErrNotImplemented
		case "estimator":
			return nil, ErrNotImplemented
		case "root":
			if ret.parent != nil {
				return nil, ErrInvalidArg
			}
			indirect := tc.HandleRoot
			ret.parent = &indirect
			// We have a one piece argument. To get to the next arg properly
			i--
		case "ingress":
			if ret.parent != nil {
				return nil, ErrInvalidArg
			}
			indirectPar := tc.HandleIngress // is the same as clsact handle
			ret.parent = &indirectPar
			// We have a one piece argument. To get to the next arg properly
			indirectHan := core.BuildHandle(tc.HandleIngress, 0)
			ret.handle = &indirectHan

			i--
		case "egress":
			if ret.parent != nil {
				return nil, ErrInvalidArg
			}
			indirectPar := tc.HandleIngress // is the same as clsact handle
			ret.parent = &indirectPar
			// We have a one piece argument. To get to the next arg properly
			indirectHan := core.BuildHandle(tc.HandleIngress, tc.HandleMinEgress)
			ret.handle = &indirectHan

			i--
		case "help":
			PrintFilterHelp(stdout)
		default: // I hope we parsed all the stuff until here
			// args[i] is the actual filter type
			// Resolve Qdisc and parameters
			var filterParse func(io.Writer, []string) (*tc.Object, error)
			var err error
			if filterParse = supportedFilters(args[i]); filterParse == nil {
				return nil, fmt.Errorf("%w: invalid filter: %s", ErrInvalidArg, args[i])
			}
			k := args[i]
			ret.kind = &k

			ret.filterObj, err = filterParse(stdout, args[i+1:])
			if err != nil {
				return nil, err
			}
			return ret, nil
		}
	}
	return ret, nil
}

func (t *Trafficctl) ShowFilter(fargs *FArgs, stdout io.Writer) error {
	iface, err := net.InterfaceByName(fargs.dev)
	if err != nil {
		return err
	}

	msg := tc.Msg{
		Family:  0,
		Ifindex: uint32(iface.Index),
	}

	filters, err := t.Tc.Filter().Get(&msg)
	if err != nil {
		return err
	}

	for _, f := range filters {
		var s strings.Builder
		fmt.Fprintf(&s, "filter parent %d: protocol: %s pref %d %s chain %d ",
			f.Parent>>16,
			GetProtoFromInfo(f.Info),
			GetPrefFromInfo(f.Info),
			f.Kind,
			*f.Chain)

		if f.Handle != 0 {
			fmt.Fprintf(&s, "handle 0x%x\n", f.Handle)
		}

		if f.Basic != nil {
			if f.Basic.Actions != nil {
				for _, act := range *f.Basic.Actions {
					fmt.Fprintf(&s, "\t\taction order %d: %s action %d\n",
						act.Index, act.Kind, act.Gact.Parms.Action)
				}
			}
		}
		fmt.Fprintf(stdout, "%s\n", s.String())
	}

	return nil
}

func (t *Trafficctl) AddFilter(fargs *FArgs, stdout io.Writer) error {
	iface, err := net.InterfaceByName(fargs.dev)
	if err != nil {
		return err
	}

	q := fargs.filterObj
	q.Ifindex = uint32(iface.Index)
	q.Handle = *fargs.handle
	q.Msg.Info = core.BuildHandle(*fargs.pref<<16, *fargs.protocol)

	fmt.Printf("%+v\n", q)

	if err := t.Tc.Filter().Add(q); err != nil {
		return err
	}
	return nil
}

func (t *Trafficctl) DeleteFilter(fargs *FArgs, stdout io.Writer) error {
	iface, err := net.InterfaceByName(fargs.dev)
	if err != nil {
		return err
	}

	msg := tc.Msg{
		Family:  0,
		Ifindex: uint32(iface.Index),
	}

	filters, err := t.Tc.Filter().Get(&msg)
	if err != nil {
		return err
	}

	if err := t.Tc.Filter().Delete(&filters[0]); err != nil {
		return err
	}

	return nil
}

func (t *Trafficctl) ReplaceFilter(fargs *FArgs, stdout io.Writer) error {
	return nil
}

func (t *Trafficctl) ChangeFilter(fargs *FArgs, stdout io.Writer) error {
	return nil
}

func (t *Trafficctl) GetFilter(fargs *FArgs, stdout io.Writer) error {
	return nil
}

const (
	Filterhelp = `Usage:
	tc filter [ add | del | change | replace | show ] [ dev STRING ]
	tc filter [ add | del | change | replace | show ] [ block BLOCK_INDEX ]
	tc filter get dev STRING parent CLASSID protocol PROTO handle FILTERID pref PRIO FILTER_TYPE
	tc filter get block BLOCK_INDEX protocol PROTO handle FILTERID pref PRIO FILTER_TYPE
		[ pref PRIO ] protocol PROTO [ chain CHAIN_INDEX ]
		[ estimator INTERVAL TIME_CONSTANT ]
		[ root | ingress | egress | parent CLASSID ]
		[ handle FILTERID ] [ [ FILTER_TYPE ] [ help | OPTIONS ] ]
	tc filter show [ dev STRING ] [ root | ingress | egress | parent CLASSID ]
	tc filter show [ block BLOCK_INDEX ]

	Where:
	FILTER_TYPE := { u32 | bpf | fw | route | etc. }
	FILTERID := ... format depends on classifier, see there
	OPTIONS := ... try tc filter add <desired FILTER_KIND> help
`
)

func PrintFilterHelp(stdout io.Writer) {
	fmt.Fprint(stdout,
		Filterhelp)
}

func supportedFilters(f string) func(io.Writer, []string) (*tc.Object, error) {
	supported := map[string]func(io.Writer, []string) (*tc.Object, error){
		"basic":    parseBasicParams,
		"bpf":      nil,
		"cgroup":   nil,
		"flow":     nil,
		"flower":   nil,
		"fw":       nil,
		"route":    nil,
		"u32":      nil,
		"matchall": nil,
	}

	ret, ok := supported[f]
	if !ok {
		return nil
	}

	return ret
}

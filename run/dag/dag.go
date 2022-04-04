// Copyright 2021 Mineiros GmbH
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dag

import (
	"fmt"
	"sort"

	"github.com/madlambda/spells/errutil"
	"github.com/rs/zerolog/log"
)

type (
	// ID of nodes
	ID string

	// DAG is a Directed-Acyclic Graph
	DAG struct {
		dag    map[ID][]ID
		values map[ID]interface{}
		cycles map[ID]bool

		validated bool
	}
)

// Errors returned by operations on the DAG.
const (
	ErrDuplicateNode errutil.Error = "duplicate node"
	ErrNodeNotFound  errutil.Error = "node not found"
	ErrCycleDetected errutil.Error = "cycle detected"
)

// New creates a new empty Directed-Acyclic-Graph.
func New() *DAG {
	return &DAG{
		dag:    make(map[ID][]ID),
		values: make(map[ID]interface{}),
	}
}

// AddNode adds a new node to the dag. The lists of before and after
// defines its edge nodes.
func (d *DAG) AddNode(id ID, value interface{}, before, after []ID) error {
	logger := log.With().
		Str("action", "AddNode()").
		Logger()

	if _, ok := d.values[id]; ok {
		return errutil.Chain(
			ErrDuplicateNode,
			fmt.Errorf("adding node id %q", id),
		)
	}

	for _, bid := range before {
		if _, ok := d.dag[bid]; !ok {
			d.dag[bid] = []ID{}
		}

		logger.Trace().
			Str("from", string(bid)).
			Str("to", string(id)).
			Msg("Add edge.")
		d.addEdge(bid, id)
	}

	if _, ok := d.dag[id]; !ok {
		d.dag[id] = []ID{}
	}

	logger.Trace().
		Str("id", string(id)).
		Msg("Add edges.")
	d.addEdges(id, after)
	d.values[id] = value
	d.validated = false
	return nil
}

func (d *DAG) addEdges(from ID, toids []ID) {
	for _, to := range toids {
		log.Trace().
			Str("action", "addEdges()").
			Str("from", string(from)).
			Str("to", string(to)).
			Msg("Add edges.")
		d.addEdge(from, to)
	}
}

func (d *DAG) addEdge(from, to ID) {
	fromEdges, ok := d.dag[from]
	if !ok {
		panic("internal error: empty list of edges must exist at this point")
	}

	if !idList(fromEdges).contains(to) {
		log.Trace().
			Str("action", "addEdge()").
			Str("from", string(from)).
			Str("to", string(to)).
			Msg("Append edge.")
		fromEdges = append(fromEdges, to)
	}

	d.dag[from] = fromEdges
}

// Validate the DAG looking for cycles.
func (d *DAG) Validate() (reason string, err error) {
	d.cycles = make(map[ID]bool)
	d.validated = true

	for _, id := range d.IDs() {
		log.Trace().
			Str("action", "Validate()").
			Str("id", string(id)).
			Msg("Validate node.")
		reason, err := d.validateNode(id, d.dag[id])
		if err != nil {
			return reason, err
		}
	}
	return "", nil
}

func (d *DAG) validateNode(id ID, children []ID) (string, error) {
	log.Trace().
		Str("action", "validateNode()").
		Str("id", string(id)).
		Msg("Check if has cycle.")
	found, reason := d.hasCycle([]ID{id}, children, fmt.Sprintf("%s ->", id))
	if found {
		d.cycles[id] = true
		return reason, errutil.Chain(
			ErrCycleDetected,
			fmt.Errorf("checking node id %q", id),
		)
	}

	return "", nil
}

func (d *DAG) hasCycle(branch []ID, children []ID, reason string) (bool, string) {
	for _, id := range branch {
		log.Trace().
			Str("action", "hasCycle()").
			Str("id", string(id)).
			Msg("Check if id is present in children.")
		if idList(children).contains(id) {
			d.cycles[id] = true
			return true, fmt.Sprintf("%s %s", reason, id)
		}
	}

	for _, tid := range sortedIds(children) {
		tlist := d.dag[tid]
		log.Trace().
			Str("action", "hasCycle()").
			Str("id", string(tid)).
			Msg("Check if id has cycle.")
		found, reason := d.hasCycle(append(branch, tid), tlist, fmt.Sprintf("%s %s ->", reason, tid))
		if found {
			return true, reason
		}
	}

	return false, ""
}

// IDs returns the sorted list of node ids.
func (d *DAG) IDs() []ID {
	idlist := make(idList, 0, len(d.dag))
	for id := range d.dag {
		idlist = append(idlist, id)
	}

	log.Trace().
		Str("action", "IDs()").
		Msg("Sort node ids.")
	sort.Sort(idlist)
	return idlist
}

// Node returns the node with the given id.
func (d *DAG) Node(id ID) (interface{}, error) {
	v, ok := d.values[id]
	if !ok {
		return nil, ErrNodeNotFound
	}
	return v, nil
}

// ChildrenOf returns the list of node ids that are children of the given id.
func (d *DAG) ChildrenOf(id ID) []ID {
	return d.dag[id]
}

// HasCycle returns true if the DAG has a cycle.
func (d *DAG) HasCycle(id ID) bool {
	if !d.validated {
		log.Trace().
			Str("action", "HasCycle()").
			Str("id", string(id)).
			Msg("Validate.")
		_, err := d.Validate()
		if err == nil {
			return false
		}
	}

	return d.cycles[id]
}

// Order returns the topological order of the DAG. The node ids are
// lexicographic sorted whenever possible to give a consistent output.
func (d *DAG) Order() []ID {
	order := []ID{}
	visited := map[ID]struct{}{}
	for _, id := range d.IDs() {
		if _, ok := visited[id]; ok {
			continue
		}
		log.Trace().
			Str("action", "Order()").
			Str("id", string(id)).
			Msg("Walk from current id.")
		d.walkFrom(id, func(id ID) {
			if _, ok := visited[id]; !ok {
				log.Trace().
					Str("action", "Order()").
					Str("id", string(id)).
					Msg("Append to ordered array.")
				order = append(order, id)
			}

			visited[id] = struct{}{}
		})

		visited[id] = struct{}{}
	}
	return order
}

func (d *DAG) walkFrom(id ID, do func(id ID)) {
	children := d.dag[id]
	for _, tid := range sortedIds(children) {
		log.Trace().
			Str("action", "walkFrom()").
			Str("id", string(id)).
			Msg("Walk from current id.")
		d.walkFrom(tid, do)
	}

	do(id)
}

func sortedIds(ids []ID) idList {
	idlist := make(idList, 0, len(ids))
	for _, id := range ids {
		idlist = append(idlist, id)
	}

	log.Trace().
		Str("action", "sortedIds()").
		Msg("Sort ids.")
	sort.Sort(idlist)
	return idlist
}

type idList []ID

func (ids idList) contains(other ID) bool {
	for _, id := range ids {
		if id == other {
			return true
		}
	}

	return false
}

func (ids idList) Len() int           { return len(ids) }
func (ids idList) Swap(i, j int)      { ids[i], ids[j] = ids[j], ids[i] }
func (ids idList) Less(i, j int) bool { return ids[i] < ids[j] }

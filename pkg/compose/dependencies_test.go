/*
   Copyright 2020 Docker Compose CLI authors

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package compose

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v2/pkg/utils"
	testify "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gotest.tools/v3/assert"
)

func createTestProject() *types.Project {
	return &types.Project{
		Services: types.Services{
			"test1": {
				Name: "test1",
				DependsOn: map[string]types.ServiceDependency{
					"test2": {},
				},
			},
			"test2": {
				Name: "test2",
				DependsOn: map[string]types.ServiceDependency{
					"test3": {},
				},
			},
			"test3": {
				Name: "test3",
			},
		},
	}
}

func TestTraversalWithMultipleParents(t *testing.T) {
	dependent := types.ServiceConfig{
		Name:      "dependent",
		DependsOn: make(types.DependsOnConfig),
	}

	project := types.Project{
		Services: types.Services{"dependent": dependent},
	}

	for i := 1; i <= 100; i++ {
		name := fmt.Sprintf("svc_%d", i)
		dependent.DependsOn[name] = types.ServiceDependency{}

		svc := types.ServiceConfig{Name: name}
		project.Services[name] = svc
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	svc := make(chan string, 10)
	seen := make(map[string]int)
	done := make(chan struct{})
	go func() {
		for service := range svc {
			seen[service]++
		}
		done <- struct{}{}
	}()

	err := InDependencyOrder(ctx, &project, func(ctx context.Context, name string, _ types.ServiceConfig) error {
		svc <- name
		return nil
	})
	require.NoError(t, err, "Error during iteration")
	close(svc)
	<-done

	testify.Len(t, seen, 101)
	for svc, count := range seen {
		assert.Equal(t, 1, count, "Service: %s", svc)
	}
}

func TestInDependencyUpCommandOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var order []string
	err := InDependencyOrder(ctx, createTestProject(), func(ctx context.Context, name string, _ types.ServiceConfig) error {
		order = append(order, name)
		return nil
	})
	require.NoError(t, err, "Error during iteration")
	require.Equal(t, []string{"test3", "test2", "test1"}, order)
}

func TestInDependencyReverseDownCommandOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var order []string
	err := InReverseDependencyOrder(ctx, createTestProject(), func(ctx context.Context, name string, _ types.ServiceConfig) error {
		order = append(order, name)
		return nil
	})
	require.NoError(t, err, "Error during iteration")
	require.Equal(t, []string{"test1", "test2", "test3"}, order)
}

func TestBuildGraph(t *testing.T) {
	testCases := []struct {
		desc             string
		services         types.Services
		expectedVertices map[string]*Vertex
	}{
		{
			desc: "builds graph with single service",
			services: types.Services{
				"test": {
					Name:      "test",
					DependsOn: types.DependsOnConfig{},
				},
			},
			expectedVertices: map[string]*Vertex{
				"test": {
					Key:      "test",
					Service:  &types.ServiceConfig{Name: "test"},
					Status:   ServiceStopped,
					Children: map[string]*Vertex{},
					Parents:  map[string]*Vertex{},
				},
			},
		},
		{
			desc: "builds graph with two separate services",
			services: types.Services{
				"test": {
					Name:      "test",
					DependsOn: types.DependsOnConfig{},
				},
				"another": {
					Name:      "another",
					DependsOn: types.DependsOnConfig{},
				},
			},
			expectedVertices: map[string]*Vertex{
				"test": {
					Key:      "test",
					Service:  &types.ServiceConfig{Name: "test"},
					Status:   ServiceStopped,
					Children: map[string]*Vertex{},
					Parents:  map[string]*Vertex{},
				},
				"another": {
					Key:      "another",
					Service:  &types.ServiceConfig{Name: "another"},
					Status:   ServiceStopped,
					Children: map[string]*Vertex{},
					Parents:  map[string]*Vertex{},
				},
			},
		},
		{
			desc: "builds graph with a service and a dependency",
			services: types.Services{
				"test": {
					Name: "test",
					DependsOn: types.DependsOnConfig{
						"another": types.ServiceDependency{},
					},
				},
				"another": {
					Name:      "another",
					DependsOn: types.DependsOnConfig{},
				},
			},
			expectedVertices: map[string]*Vertex{
				"test": {
					Key:     "test",
					Service: &types.ServiceConfig{Name: "test"},
					Status:  ServiceStopped,
					Children: map[string]*Vertex{
						"another": {},
					},
					Parents: map[string]*Vertex{},
				},
				"another": {
					Key:      "another",
					Service:  &types.ServiceConfig{Name: "another"},
					Status:   ServiceStopped,
					Children: map[string]*Vertex{},
					Parents: map[string]*Vertex{
						"test": {},
					},
				},
			},
		},
		{
			desc: "builds graph with multiple dependency levels",
			services: types.Services{
				"test": {
					Name: "test",
					DependsOn: types.DependsOnConfig{
						"another": types.ServiceDependency{},
					},
				},
				"another": {
					Name: "another",
					DependsOn: types.DependsOnConfig{
						"another_dep": types.ServiceDependency{},
					},
				},
				"another_dep": {
					Name:      "another_dep",
					DependsOn: types.DependsOnConfig{},
				},
			},
			expectedVertices: map[string]*Vertex{
				"test": {
					Key:     "test",
					Service: &types.ServiceConfig{Name: "test"},
					Status:  ServiceStopped,
					Children: map[string]*Vertex{
						"another": {},
					},
					Parents: map[string]*Vertex{},
				},
				"another": {
					Key:     "another",
					Service: &types.ServiceConfig{Name: "another"},
					Status:  ServiceStopped,
					Children: map[string]*Vertex{
						"another_dep": {},
					},
					Parents: map[string]*Vertex{
						"test": {},
					},
				},
				"another_dep": {
					Key:      "another_dep",
					Service:  &types.ServiceConfig{Name: "another_dep"},
					Status:   ServiceStopped,
					Children: map[string]*Vertex{},
					Parents: map[string]*Vertex{
						"another": {},
					},
				},
			},
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			project := types.Project{
				Services: tC.services,
			}

			graph, err := NewGraph(&project, ServiceStopped)
			assert.NilError(t, err, fmt.Sprintf("failed to build graph for: %s", tC.desc))

			for k, vertex := range graph.Vertices {
				expected, ok := tC.expectedVertices[k]
				assert.Equal(t, true, ok)
				assertVertexEqual(t, *expected, *vertex)
			}
		})
	}
}

func assertVertexEqual(t *testing.T, a, b Vertex) {
	assert.Equal(t, a.Key, b.Key)
	assert.Equal(t, a.Service.Name, b.Service.Name)
	for c := range a.Children {
		_, ok := b.Children[c]
		assert.Check(t, ok, "expected children missing %s", c)
	}
	for p := range a.Parents {
		_, ok := b.Parents[p]
		assert.Check(t, ok, "expected parent missing %s", p)
	}
}

func TestWith_RootNodesAndUp(t *testing.T) {
	graph := &Graph{
		lock:     sync.RWMutex{},
		Vertices: map[string]*Vertex{},
	}

	/** graph topology:
	           A   B
		      / \ / \
		     G   C   E
		          \ /
		           D
		           |
		           F
	*/

	graph.addVertex("A", types.ServiceConfig{Name: "A"}, 0)
	graph.addVertex("B", types.ServiceConfig{Name: "B"}, 0)
	graph.addVertex("C", types.ServiceConfig{Name: "C"}, 0)
	graph.addVertex("D", types.ServiceConfig{Name: "D"}, 0)
	graph.addVertex("E", types.ServiceConfig{Name: "E"}, 0)
	graph.addVertex("F", types.ServiceConfig{Name: "F"}, 0)
	graph.addVertex("G", types.ServiceConfig{Name: "G"}, 0)

	_ = graph.addEdge("C", "A")
	_ = graph.addEdge("C", "B")
	_ = graph.addEdge("E", "B")
	_ = graph.addEdge("D", "C")
	_ = graph.addEdge("D", "E")
	_ = graph.addEdge("F", "D")
	_ = graph.addEdge("G", "A")

	tests := []struct {
		name  string
		nodes []string
		want  []string
	}{
		{
			name:  "whole graph",
			nodes: []string{"A", "B"},
			want:  []string{"A", "B", "C", "D", "E", "F", "G"},
		},
		{
			name:  "only leaves",
			nodes: []string{"F", "G"},
			want:  []string{"F", "G"},
		},
		{
			name:  "simple dependent",
			nodes: []string{"D"},
			want:  []string{"D", "F"},
		},
		{
			name:  "diamond dependents",
			nodes: []string{"B"},
			want:  []string{"B", "C", "D", "E", "F"},
		},
		{
			name:  "partial graph",
			nodes: []string{"A"},
			want:  []string{"A", "C", "D", "F", "G"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mx := sync.Mutex{}
			expected := utils.Set[string]{}
			expected.AddAll("C", "G", "D", "F")
			var visited []string

			gt := downDirectionTraversal(func(ctx context.Context, name string, service types.ServiceConfig) error {
				mx.Lock()
				defer mx.Unlock()
				visited = append(visited, name)
				return nil
			})
			WithRootNodesAndDown(tt.nodes)(gt)
			err := gt.visit(context.TODO(), graph)
			assert.NilError(t, err)
			sort.Strings(visited)
			assert.DeepEqual(t, tt.want, visited)
		})
	}
}

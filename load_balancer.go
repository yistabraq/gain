// Copyright (c) 2023 Paweł Gaczyński
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gain

type LoadBalancing int

const (
	RoundRobin LoadBalancing = iota
	LeastConnections
	SourceIPHash
)

type loadBalancer interface {
	register(consumer)
	next() consumer
	forEach(func(consumer) error) error
}

type genericLoadBalancer struct {
	workers []consumer
	size    int
}

func (b *genericLoadBalancer) register(worker consumer) {
	worker.setIndex(b.size)
	b.workers = append(b.workers, worker)
	b.size++
}

type roundRobinLoadBalancer struct {
	*genericLoadBalancer
	nextWorkerIndex int
}

func (b *roundRobinLoadBalancer) next() consumer {
	c := b.workers[b.nextWorkerIndex]
	if b.nextWorkerIndex++; b.nextWorkerIndex >= b.size {
		b.nextWorkerIndex = 0
	}
	return c
}

func (b *roundRobinLoadBalancer) forEach(callback func(consumer) error) error {
	for _, c := range b.workers {
		err := callback(c)
		if err != nil {
			return err
		}
	}
	return nil
}

func newRoundRobinLoadBalancer() loadBalancer {
	return &roundRobinLoadBalancer{
		genericLoadBalancer: &genericLoadBalancer{},
	}
}

type leastConnectionsLoadBalancer struct {
	*genericLoadBalancer
}

func (b *leastConnectionsLoadBalancer) next() consumer {
	worker := b.workers[0]
	minN := worker.activeConnections()
	for _, v := range b.workers[1:] {
		if n := v.activeConnections(); n < minN {
			minN = n
			worker = v
		}
	}
	return worker
}

func (b *leastConnectionsLoadBalancer) forEach(callback func(consumer) error) error {
	for _, c := range b.workers {
		err := callback(c)
		if err != nil {
			return err
		}
	}
	return nil
}

func newLeastConnectionsLoadBalancer() loadBalancer {
	return &leastConnectionsLoadBalancer{
		genericLoadBalancer: &genericLoadBalancer{},
	}
}

func createLoadBalancer(loadBalancing LoadBalancing) (loadBalancer, error) {
	switch loadBalancing {
	case RoundRobin:
		return newRoundRobinLoadBalancer(), nil
	case LeastConnections:
		return newLeastConnectionsLoadBalancer(), nil
	case SourceIPHash:
		return nil, errNotImplemented
	default:
		return nil, errNotSupported
	}
}

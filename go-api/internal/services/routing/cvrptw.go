package routing

import (
	"fmt"
	"log"
	"math"
	"time"
)

// ============================================================================
// CVRPTW Solver — Capacitated Vehicle Routing Problem with Time Windows
// ============================================================================
// Solves the multi-stop routing optimization for food delivery:
//   INPUT:  Distance/duration matrices, vehicle capacities, time windows
//   OUTPUT: Optimal routes for each courier (vehicle)
//
// Algorithm:
//   Phase 1: Construction — Modified Nearest Neighbor with feasibility checks
//   Phase 2: Intra-route — 2-opt local search improvement
//   Phase 3: Inter-route — Relocate nodes between routes
// ============================================================================

// DeliveryNode represents a school to deliver to.
// Index 0 is always the depot (kitchen).
type DeliveryNode struct {
	Index           int
	SchoolID        string
	SchoolName      string
	Area            string   // Kecamatan area for traffic lookup
	Demand          int       // portions to deliver
	TimeWindowStart time.Time // earliest arrival
	TimeWindowEnd   time.Time // latest arrival (from shelf-life deadline)
	ServiceTime     int       // seconds to unload at school (default: 300s = 5min)
	Lat             float64
	Lng             float64
}

// VehicleInfo represents a courier's vehicle constraints.
type VehicleInfo struct {
	ID        string
	CourierID string
	Capacity  int // max portions
}

// SolverParams contains all input for the CVRPTW solver.
type SolverParams struct {
	DurationMatrix [][]int       // seconds between all node pairs (traffic-adjusted)
	DistanceMatrix [][]int       // meters between all node pairs
	Nodes          []DeliveryNode // index 0 = depot, 1..n = schools
	Vehicles       []VehicleInfo
	DepotIndex     int           // always 0
	DepartureTime  time.Time     // planned departure from kitchen
	Deadline       time.Time     // hard deadline (cooked_at + max_delivery_window)
	MaxIterations  int           // for improvement phase
}

// SolverResult contains the optimized routes.
type SolverResult struct {
	Routes        []VehicleRoute
	TotalDistance  int
	TotalDuration int
	Feasible      bool
	UnservedNodes []int
	Score         float64
	Iterations    int
}

// VehicleRoute is the complete route for one courier.
type VehicleRoute struct {
	VehicleID   string
	CourierID   string
	Stops       []StopResult
	TotalLoad   int
	TotalDist   int
	TotalTime   int
	DepartureAt time.Time
	LastArrival time.Time
}

// StopResult is a single stop in a route with timing info.
type StopResult struct {
	NodeIndex   int
	SchoolID    string
	SchoolName  string
	Sequence    int
	ArrivalTime time.Time
	DepartTime  time.Time
	Portions    int
	CumLoad     int
	CumDist     int
	CumTime     int
}

// ============================================================================
// PUBLIC API
// ============================================================================

// Solve executes the CVRPTW algorithm and returns optimized routes.
func Solve(params SolverParams) (*SolverResult, error) {
	n := len(params.Nodes)
	if n < 2 {
		return nil, fmt.Errorf("need at least 1 delivery node plus depot (got %d total)", n)
	}
	if len(params.Vehicles) == 0 {
		return nil, fmt.Errorf("need at least 1 vehicle")
	}
	if len(params.DurationMatrix) != n || len(params.DistanceMatrix) != n {
		return nil, fmt.Errorf("matrix dimensions (%d) don't match node count (%d)",
			len(params.DurationMatrix), n)
	}

	if params.MaxIterations <= 0 {
		params.MaxIterations = 1000
	}

	log.Printf("🚛 [CVRPTW] Starting solver: %d schools, %d vehicles, deadline: %s",
		n-1, len(params.Vehicles), params.Deadline.Format("15:04:05"))

	// Phase 1: Construction — build initial feasible routes
	routes := constructRoutes(params)
	log.Printf("🚛 [CVRPTW] Phase 1 (Construction): %d routes created", len(routes))

	// Phase 2: Intra-route improvement — 2-opt within each route
	iterations := 0
	routes, iterations = improveRoutes(routes, params)
	log.Printf("🚛 [CVRPTW] Phase 2 (2-opt): %d improvement iterations", iterations)

	// Phase 3: Inter-route optimization — relocate nodes between routes
	routes = interRouteRelocate(routes, params)
	log.Printf("🚛 [CVRPTW] Phase 3 (Relocate): completed")

	// Collect results
	served := make(map[int]bool)
	totalDist, totalTime := 0, 0
	for i := range routes {
		recalculateRoute(&routes[i], params)
		for _, s := range routes[i].Stops {
			served[s.NodeIndex] = true
		}
		totalDist += routes[i].TotalDist
		totalTime += routes[i].TotalTime
	}

	var unserved []int
	for i := 1; i < n; i++ {
		if !served[i] {
			unserved = append(unserved, i)
		}
	}

	feasible := len(unserved) == 0
	score := calculateScore(routes, params, feasible)

	if !feasible {
		log.Printf("⚠️  [CVRPTW] WARNING: %d schools could not be served! Consider adding more couriers.", len(unserved))
	}

	log.Printf("🚛 [CVRPTW] Solution: %d routes, total distance: %dm, feasible: %v, score: %.2f",
		len(routes), totalDist, feasible, score)

	return &SolverResult{
		Routes:        routes,
		TotalDistance:  totalDist,
		TotalDuration: totalTime,
		Feasible:      feasible,
		UnservedNodes: unserved,
		Score:         score,
		Iterations:    iterations,
	}, nil
}

// ============================================================================
// PHASE 1: Construction — Modified Nearest Neighbor
// ============================================================================

func constructRoutes(params SolverParams) []VehicleRoute {
	n := len(params.Nodes)
	visited := make([]bool, n)
	visited[params.DepotIndex] = true // depot doesn't need visiting

	var routes []VehicleRoute

	for _, vehicle := range params.Vehicles {
		route := VehicleRoute{
			VehicleID:   vehicle.ID,
			CourierID:   vehicle.CourierID,
			DepartureAt: params.DepartureTime,
		}

		currentNode := params.DepotIndex
		currentTime := params.DepartureTime
		currentLoad := 0

		for {
			bestNode := -1
			bestDuration := math.MaxInt64

			// Find nearest feasible unvisited node
			for i := 1; i < n; i++ {
				if visited[i] {
					continue
				}

				node := params.Nodes[i]
				travelTime := params.DurationMatrix[currentNode][i]

				// Check capacity constraint
				if currentLoad+node.Demand > vehicle.Capacity {
					continue
				}

				// Check time window constraint
				arrivalTime := currentTime.Add(time.Duration(travelTime) * time.Second)

				// Can't arrive after the time window closes
				if arrivalTime.After(node.TimeWindowEnd) {
					continue
				}

				// If we arrive early, we wait (but it still counts against deadline)
				if arrivalTime.Before(node.TimeWindowStart) {
					arrivalTime = node.TimeWindowStart
				}

				// Check if we'd still be within the global deadline after service
				departAfterService := arrivalTime.Add(time.Duration(node.ServiceTime) * time.Second)
				if departAfterService.After(params.Deadline) {
					continue
				}

				// Prefer nearest (shortest duration)
				if travelTime < bestDuration {
					bestDuration = travelTime
					bestNode = i
				}
			}

			// No more feasible nodes for this vehicle
			if bestNode == -1 {
				break
			}

			// Add the best node to route
			node := params.Nodes[bestNode]
			travelTime := params.DurationMatrix[currentNode][bestNode]
			arrivalTime := currentTime.Add(time.Duration(travelTime) * time.Second)

			// Wait if early
			if arrivalTime.Before(node.TimeWindowStart) {
				arrivalTime = node.TimeWindowStart
			}

			departTime := arrivalTime.Add(time.Duration(node.ServiceTime) * time.Second)
			currentLoad += node.Demand

			stop := StopResult{
				NodeIndex:   bestNode,
				SchoolID:    node.SchoolID,
				SchoolName:  node.SchoolName,
				Sequence:    len(route.Stops) + 1,
				ArrivalTime: arrivalTime,
				DepartTime:  departTime,
				Portions:    node.Demand,
				CumLoad:     currentLoad,
				CumDist:     route.TotalDist + params.DistanceMatrix[currentNode][bestNode],
				CumTime:     int(departTime.Sub(params.DepartureTime).Seconds()),
			}

			route.Stops = append(route.Stops, stop)
			route.TotalDist += params.DistanceMatrix[currentNode][bestNode]
			route.TotalLoad = currentLoad

			visited[bestNode] = true
			currentNode = bestNode
			currentTime = departTime
		}

		// Only add route if it has stops
		if len(route.Stops) > 0 {
			route.TotalTime = int(currentTime.Sub(params.DepartureTime).Seconds())
			route.LastArrival = route.Stops[len(route.Stops)-1].ArrivalTime
			routes = append(routes, route)
		}

		// Check if all nodes are served
		allServed := true
		for i := 1; i < n; i++ {
			if !visited[i] {
				allServed = false
				break
			}
		}
		if allServed {
			break
		}
	}

	return routes
}

// ============================================================================
// PHASE 2: Intra-route 2-opt Improvement
// ============================================================================

func improveRoutes(routes []VehicleRoute, params SolverParams) ([]VehicleRoute, int) {
	totalIterations := 0

	for r := range routes {
		if len(routes[r].Stops) < 2 {
			continue
		}

		improved := true
		for improved && totalIterations < params.MaxIterations {
			improved = false
			stops := routes[r].Stops

			for i := 0; i < len(stops)-1; i++ {
				for j := i + 1; j < len(stops); j++ {
					totalIterations++

					// Try reversing segment [i+1..j]
					newStops := make([]StopResult, len(stops))
					copy(newStops, stops)
					reverseSegment(newStops, i+1, j)

					// Check if new order is feasible and better
					newDist := calculateRouteDist(newStops, params)
					oldDist := calculateRouteDist(stops, params)

					if newDist < oldDist && isRouteFeasible(newStops, params, routes[r]) {
						routes[r].Stops = newStops
						stops = newStops
						improved = true
					}
				}
			}
		}

		// Recalculate timings after improvement
		recalculateRoute(&routes[r], params)
	}

	return routes, totalIterations
}

// ============================================================================
// PHASE 3: Inter-route Relocate
// ============================================================================

func interRouteRelocate(routes []VehicleRoute, params SolverParams) []VehicleRoute {
	if len(routes) < 2 {
		return routes
	}

	improved := true
	maxIter := 200
	iter := 0

	for improved && iter < maxIter {
		improved = false
		iter++

		for r1 := 0; r1 < len(routes); r1++ {
			for r2 := 0; r2 < len(routes); r2++ {
				if r1 == r2 {
					continue
				}

				// Try moving each node from route r1 to best position in route r2
				for i := 0; i < len(routes[r1].Stops); i++ {
					node := routes[r1].Stops[i]
					demand := params.Nodes[node.NodeIndex].Demand

					// Check if r2 has capacity
					vehicleIdx := findVehicleIdx(params.Vehicles, routes[r2].VehicleID)
					if vehicleIdx < 0 {
						continue
					}
					if routes[r2].TotalLoad+demand > params.Vehicles[vehicleIdx].Capacity {
						continue
					}

					// Try inserting at best position in r2
					bestPos := -1
					bestCost := math.MaxInt64

					for j := 0; j <= len(routes[r2].Stops); j++ {
						testStops := insertStop(routes[r2].Stops, j, node)
						cost := calculateRouteDist(testStops, params)
						if cost < bestCost && isRouteFeasible(testStops, params, routes[r2]) {
							bestCost = cost
							bestPos = j
						}
					}

					if bestPos >= 0 {
						origR1Cost := calculateRouteDist(routes[r1].Stops, params)
						origR2Cost := calculateRouteDist(routes[r2].Stops, params)

						newR1Stops := removeStop(routes[r1].Stops, i)
						newR2Stops := insertStop(routes[r2].Stops, bestPos, node)

						newR1Cost := calculateRouteDist(newR1Stops, params)

						if newR1Cost+bestCost < origR1Cost+origR2Cost {
							routes[r1].Stops = newR1Stops
							routes[r2].Stops = newR2Stops
							recalculateRoute(&routes[r1], params)
							recalculateRoute(&routes[r2], params)
							improved = true
							break
						}
					}
				}
				if improved {
					break
				}
			}
			if improved {
				break
			}
		}
	}

	// Remove empty routes
	var nonEmpty []VehicleRoute
	for _, r := range routes {
		if len(r.Stops) > 0 {
			nonEmpty = append(nonEmpty, r)
		}
	}

	return nonEmpty
}

// ============================================================================
// Helper Functions
// ============================================================================

func reverseSegment(stops []StopResult, i, j int) {
	for i < j {
		stops[i], stops[j] = stops[j], stops[i]
		i++
		j--
	}
}

func calculateRouteDist(stops []StopResult, params SolverParams) int {
	if len(stops) == 0 {
		return 0
	}

	totalDist := params.DistanceMatrix[params.DepotIndex][stops[0].NodeIndex]
	for i := 1; i < len(stops); i++ {
		totalDist += params.DistanceMatrix[stops[i-1].NodeIndex][stops[i].NodeIndex]
	}
	return totalDist
}

func isRouteFeasible(stops []StopResult, params SolverParams, route VehicleRoute) bool {
	if len(stops) == 0 {
		return true
	}

	currentTime := params.DepartureTime
	currentNode := params.DepotIndex
	load := 0

	vehicleIdx := findVehicleIdx(params.Vehicles, route.VehicleID)
	capacity := 9999
	if vehicleIdx >= 0 {
		capacity = params.Vehicles[vehicleIdx].Capacity
	}

	for _, stop := range stops {
		node := params.Nodes[stop.NodeIndex]
		travelTime := params.DurationMatrix[currentNode][stop.NodeIndex]

		arrivalTime := currentTime.Add(time.Duration(travelTime) * time.Second)

		// Wait if early
		if arrivalTime.Before(node.TimeWindowStart) {
			arrivalTime = node.TimeWindowStart
		}

		// Time window violated
		if arrivalTime.After(node.TimeWindowEnd) {
			return false
		}

		// Global deadline violated
		departTime := arrivalTime.Add(time.Duration(node.ServiceTime) * time.Second)
		if departTime.After(params.Deadline) {
			return false
		}

		load += node.Demand
		if load > capacity {
			return false
		}

		currentNode = stop.NodeIndex
		currentTime = departTime
	}

	return true
}

func recalculateRoute(route *VehicleRoute, params SolverParams) {
	if len(route.Stops) == 0 {
		route.TotalDist = 0
		route.TotalTime = 0
		route.TotalLoad = 0
		return
	}

	currentTime := params.DepartureTime
	currentNode := params.DepotIndex
	totalDist := 0
	totalLoad := 0

	for i := range route.Stops {
		node := params.Nodes[route.Stops[i].NodeIndex]
		travelTime := params.DurationMatrix[currentNode][route.Stops[i].NodeIndex]
		travelDist := params.DistanceMatrix[currentNode][route.Stops[i].NodeIndex]

		arrivalTime := currentTime.Add(time.Duration(travelTime) * time.Second)
		if arrivalTime.Before(node.TimeWindowStart) {
			arrivalTime = node.TimeWindowStart
		}

		departTime := arrivalTime.Add(time.Duration(node.ServiceTime) * time.Second)

		totalDist += travelDist
		totalLoad += node.Demand

		route.Stops[i].Sequence = i + 1
		route.Stops[i].ArrivalTime = arrivalTime
		route.Stops[i].DepartTime = departTime
		route.Stops[i].Portions = node.Demand
		route.Stops[i].CumLoad = totalLoad
		route.Stops[i].CumDist = totalDist
		route.Stops[i].CumTime = int(departTime.Sub(params.DepartureTime).Seconds())

		currentNode = route.Stops[i].NodeIndex
		currentTime = departTime
	}

	route.TotalDist = totalDist
	route.TotalTime = int(currentTime.Sub(params.DepartureTime).Seconds())
	route.TotalLoad = totalLoad
	if len(route.Stops) > 0 {
		route.LastArrival = route.Stops[len(route.Stops)-1].ArrivalTime
	}
}

func insertStop(stops []StopResult, pos int, stop StopResult) []StopResult {
	result := make([]StopResult, len(stops)+1)
	copy(result[:pos], stops[:pos])
	result[pos] = stop
	copy(result[pos+1:], stops[pos:])
	return result
}

func removeStop(stops []StopResult, pos int) []StopResult {
	result := make([]StopResult, 0, len(stops)-1)
	result = append(result, stops[:pos]...)
	result = append(result, stops[pos+1:]...)
	return result
}

func findVehicleIdx(vehicles []VehicleInfo, vehicleID string) int {
	for i, v := range vehicles {
		if v.ID == vehicleID {
			return i
		}
	}
	return -1
}

func calculateScore(routes []VehicleRoute, params SolverParams, feasible bool) float64 {
	if !feasible {
		return 0
	}

	// Score based on: fewer vehicles used + shorter total distance + time efficiency
	score := 100.0

	// Penalty for each extra vehicle (-5 per vehicle beyond 1)
	score -= float64(len(routes)-1) * 5.0

	// Penalty for total distance (normalized)
	totalDist := 0
	for _, r := range routes {
		totalDist += r.TotalDist
	}
	score -= float64(totalDist) / 10000.0 // -1 point per 10km

	// Bonus for time efficiency (how far from deadline)
	for _, r := range routes {
		if len(r.Stops) > 0 {
			lastArrival := r.Stops[len(r.Stops)-1].ArrivalTime
			timeToDeadline := params.Deadline.Sub(lastArrival)
			if timeToDeadline > 0 {
				score += timeToDeadline.Minutes() * 0.5 // +0.5 per minute of buffer
			}
		}
	}

	return math.Max(0, math.Min(100, score))
}

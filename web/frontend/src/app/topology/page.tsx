"use client"

import { useEffect, useRef, useState } from "react"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Badge, healthVariant } from "@/components/ui/badge"
import type { ServiceGraph, ServiceNode } from "@/lib/api"

// Demo graph mirroring the backend mock topology so the view is meaningful
// before the ARMS integration is wired to live data.
const DEMO_GRAPH: ServiceGraph = {
  nodes: [
    { id: "api-gateway", name: "api-gateway", type: "service", health: "healthy", error_rate: 0.001, latency: 45.5, throughput: 1250 },
    { id: "order-service", name: "order-service", type: "service", health: "degraded", error_rate: 0.025, latency: 120.5, throughput: 850 },
    { id: "payment-service", name: "payment-service", type: "service", health: "healthy", error_rate: 0.002, latency: 85, throughput: 320 },
    { id: "user-service", name: "user-service", type: "service", health: "healthy", error_rate: 0.001, latency: 35, throughput: 1500 },
    { id: "postgres-order", name: "postgres-order", type: "database", health: "degraded", error_rate: 0.015, latency: 25.5, throughput: 850 },
    { id: "redis-cache", name: "redis-cache", type: "cache", health: "healthy", error_rate: 0, latency: 2.5, throughput: 2500 },
  ],
  edges: [
    { id: "e1", from_node_id: "api-gateway", to_node_id: "order-service", type: "http", error_rate: 0.025, latency: 120.5, throughput: 850 },
    { id: "e2", from_node_id: "api-gateway", to_node_id: "payment-service", type: "http", error_rate: 0.002, latency: 85, throughput: 320 },
    { id: "e3", from_node_id: "api-gateway", to_node_id: "user-service", type: "http", error_rate: 0.001, latency: 35, throughput: 1500 },
    { id: "e4", from_node_id: "order-service", to_node_id: "postgres-order", type: "database", error_rate: 0.015, latency: 25.5, throughput: 1200 },
    { id: "e5", from_node_id: "order-service", to_node_id: "redis-cache", type: "cache", error_rate: 0, latency: 2.5, throughput: 5000 },
    { id: "e6", from_node_id: "user-service", to_node_id: "redis-cache", type: "cache", error_rate: 0, latency: 2, throughput: 20000 },
  ],
}

const HEALTH_COLOR: Record<string, string> = {
  healthy: "#22c55e",
  degraded: "#eab308",
  unhealthy: "#ef4444",
  unknown: "#64748b",
}

const TYPE_SHAPE: Record<string, string> = {
  service: "round-rectangle",
  database: "cylinder",
  cache: "diamond",
}

export default function TopologyPage() {
  const containerRef = useRef<HTMLDivElement>(null)
  const [graph, setGraph] = useState<ServiceGraph>(DEMO_GRAPH)
  const [selected, setSelected] = useState<ServiceNode | null>(null)

  useEffect(() => {
    let disposed = false
    // Prefer live data, fall back to demo.
    import("@/lib/api").then(({ api }) => {
      api.getTopology().then((res) => {
        if (disposed) return
        const g = res.topology
        if (g && g.nodes && g.nodes.length > 0) setGraph(g)
      }).catch(() => {
        /* keep demo */
      })
    })
    return () => {
      disposed = true
    }
  }, [])

  useEffect(() => {
    if (!containerRef.current) return
    let cy: { destroy: () => void } | null = null

    import("cytoscape").then((mod) => {
      const cytoscape = mod.default
      const elements: object[] = []

      graph.nodes.forEach((n) => {
        elements.push({
          data: {
            id: n.id,
            label: n.name,
            health: n.health,
            type: n.type,
          },
        })
      })

      graph.edges.forEach((e) => {
        elements.push({
          data: {
            id: e.id,
            source: e.from_node_id,
            target: e.to_node_id,
            errorRate: e.error_rate,
          },
        })
      })

      cy = cytoscape({
        container: containerRef.current,
        elements,
        style: [
          {
            selector: "node",
            style: {
              label: "data(label)",
              "background-color": (ele: { data: (k: string) => string }) =>
                HEALTH_COLOR[ele.data("health")] ?? HEALTH_COLOR.unknown,
              shape: (ele: { data: (k: string) => string }) =>
                TYPE_SHAPE[ele.data("type")] ?? "ellipse",
              color: "#e2e8f0",
              "font-size": 10,
              width: 44,
              height: 44,
              "text-valign": "bottom",
              "text-margin-y": 6,
            },
          },
          {
            selector: "edge",
            style: {
              width: 2,
              "line-color": "#334155",
              "target-arrow-color": "#475569",
              "target-arrow-shape": "triangle",
              "curve-style": "bezier",
            },
          },
          {
            selector: "edge[errorRate > 0.01]",
            style: {
              "line-color": "#ef4444",
              "target-arrow-color": "#ef4444",
              width: 3,
            },
          },
        ],
        layout: { name: "breadthfirst", directed: true, padding: 24 } as object,
      })

      cy.on("tap", "node", (_evt: unknown) => {
        const node = graph.nodes.find(
          (n) => n.id === (_evt as { target: { id: () => string } }).target.id()
        )
        if (node) setSelected(node)
      })
    })

    return () => {
      cy?.destroy()
    }
  }, [graph])

  return (
    <div className="space-y-8">
      <header>
        <h1 className="text-3xl font-bold tracking-tight">Topology</h1>
        <p className="text-muted-foreground">
          服务依赖拓扑 — 红色边表示错误率 &gt; 1%，点击节点查看详情
        </p>
      </header>

      <div className="grid gap-6 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle>Service Graph</CardTitle>
            <CardDescription>ARMS Service Graph 集成视图</CardDescription>
          </CardHeader>
          <CardContent>
            <div
              ref={containerRef}
              className="h-[480px] w-full rounded-md border bg-background"
            />
            <div className="mt-3 flex gap-4 text-xs text-muted-foreground">
              {Object.entries(HEALTH_COLOR).map(([k, v]) => (
                <span key={k} className="flex items-center gap-1.5">
                  <span
                    className="inline-block h-3 w-3 rounded-full"
                    style={{ background: v }}
                  />
                  {k}
                </span>
              ))}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{selected ? selected.name : "节点详情"}</CardTitle>
            <CardDescription>
              {selected ? selected.type : "点击拓扑中的节点"}
            </CardDescription>
          </CardHeader>
          <CardContent>
            {selected ? (
              <div className="space-y-4">
                <div className="flex items-center justify-between">
                  <span className="text-sm text-muted-foreground">健康</span>
                  <Badge variant={healthVariant(selected.health)}>
                    {selected.health}
                  </Badge>
                </div>
                <MetricRow label="错误率" value={`${(selected.error_rate * 100).toFixed(2)}%`} />
                <MetricRow label="延迟" value={`${selected.latency.toFixed(1)} ms`} />
                <MetricRow label="吞吐" value={`${selected.throughput.toFixed(0)} rps`} />
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">
                选择一个节点查看健康与流量指标。
              </p>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

function MetricRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between border-b border-border/50 pb-2">
      <span className="text-sm text-muted-foreground">{label}</span>
      <span className="font-mono text-sm font-medium">{value}</span>
    </div>
  )
}

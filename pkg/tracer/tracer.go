package tracer

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// InitTracer 初始化全局 Tracer
// serviceName: 服务名称
// collectorAddr: Jaeger/OTEL Collector 地址
func InitTracer(serviceName string, collectorAddr string) (*sdktrace.TracerProvider, error) {
	ctx := context.Background()

	// 1. 创建 OTLP Exporter (通过 gRPC 发送数据到 Jaeger)
	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(collectorAddr), otlptracegrpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	// 2. 创建 Resource（包含服务信息）
	res, err := resource.New(ctx, 
		resource.WithAttributes(
			semconv.ServiceName(serviceName), 
			semconv.ServiceVersion("v0.1.0"),
		),
	)
	if err != nil {
		return nil, err
	}

	// 3. 创建 TracerProvider
	// 采样率更新为 0.1
	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.1))

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// 4. 设置全局 TracerProvider
	otel.SetTracerProvider(tp)

	// 5. 设置全局 Propagator (用于跨服务传播 Context)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp, nil
}
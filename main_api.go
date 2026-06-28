package main

import (
	"bytes"
	"encoding/json" // 🌟 Penting: Ditambahkan untuk encoding JSON yang aman
	"fmt"
	"io"
	"net/http"
	"time"

	// 🌟 MENAMBAHKAN LIBRARY APM TRACING DATADOG
	httptrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/net/http"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

func sendMetricToDatadog(metricName string, value float64, metricTypeStr string) {
	apiKey := "589626d4e8e7a50c4a786c41df5073bd"
	url := "https://api.us5.datadoghq.com/api/v2/series"

	metricTypeInt := 1 // Default COUNT
	if metricTypeStr == "3" {
		metricTypeInt = 4 // 🌟 Diubah ke 4 (DISTRIBUTION) agar support p50, p90, p95 asli
	}

	currentTimestamp := time.Now().Unix()

	// 🌟 Menyusun payload menggunakan struktur map asli Go agar format JSON anti-gagal
	payloadObj := map[string]interface{}{
		"series": []map[string]interface{}{
			{
				"metric": metricName,
				"type":   metricTypeInt,
				"points": []map[string]interface{}{
					{
						"timestamp": currentTimestamp,
						"value":     value,
					},
				},
				"tags": []string{"env:development", "service:l1-simulator-service"},
			},
		},
	}

	// Otomatis mengonversi objek menjadi byte JSON yang valid
	jsonPayload, err := json.Marshal(payloadObj)
	if err != nil {
		fmt.Printf("❌ Gagal menyusun JSON untuk %s: %v\n", metricName, err)
		return
	}

	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("DD-API-KEY", apiKey)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)

	if err != nil {
		fmt.Printf("❌ Gagal %s: %v\n", metricName, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 202 || resp.StatusCode == 200 {
		fmt.Printf("✅ %s sukses masuk Datadog!\n", metricName)
	} else {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("⚠️ %s ditolak! Status: %d, Balasan: %s\n", metricName, resp.StatusCode, string(body))
	}
}

func main() {
	// 🌟 1. NYALAKAN CORE TRACER DATADOG APM SAAT APLIKASI START
	tracer.Start(
		tracer.WithService("l1-simulator-service"),
		tracer.WithEnv("development"),
		tracer.WithServiceVersion("1.0.0"),
	)
	defer tracer.Stop() // Memastikan sisa data tracing di memory ter-flush habis saat aplikasi stop

	// 🌟 2. BUNGKUS SERVE MUX BAWAAN DENGAN HTTPTRACE BIAR OTOMATIS ME-RECORD REQUEST
	mux := httptrace.NewServeMux()

	// 1. Endpoint Normal
	mux.HandleFunc("/normal", func(w http.ResponseWriter, r *http.Request) {
		sendMetricToDatadog("l1.simulator.rps", 1, "1")
		sendMetricToDatadog("l1.simulator.error", 0, "1")
		sendMetricToDatadog("l1.simulator.latency", 0.05, "3") // Latency rendah (0.05s)
		w.Write([]byte("Trafik HTTP API Sukses!"))
	})

	// 2. Endpoint Slow (Untuk memancing grafik Latency melonjak)
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		// 🌟 3. BUAT CHILD SPAN UNTUK SIMULASI PROSES INTERNAL YANG LEMOT
		span, _ := tracer.StartSpanFromContext(r.Context(), "heavy_db_query")
		
		sendMetricToDatadog("l1.simulator.rps", 1, "1")
		sendMetricToDatadog("l1.simulator.error", 0, "1")
		sendMetricToDatadog("l1.simulator.latency", 2.5, "3") // Latency tinggi (2.5s)
		
		time.Sleep(1 * time.Second)
		
		span.Finish() // 🌟 SELESAI REKAM SPAN INTERNAL

		w.Write([]byte("Trafik Lemot HTTP API Sukses!"))
	})

	// 3. Endpoint Error (Untuk memancing grafik Error Rate melonjak)
	mux.HandleFunc("/error", func(w http.ResponseWriter, r *http.Request) {
		// 🌟 4. BUAT SPAN LOGIC ERROR DAN BERI TAG ERROR AGAR TERLIHAT MERAH DI APM DASHBOARD
		span, _ := tracer.StartSpanFromContext(r.Context(), "logic.process.error")
		span.SetTag("error", true)
		span.SetTag("error.msg", "Sengaja dibikin error oleh simulator")
		defer span.Finish()

		sendMetricToDatadog("l1.simulator.rps", 1, "1")
		sendMetricToDatadog("l1.simulator.error", 1, "1")      // Error dihitung 1
		sendMetricToDatadog("l1.simulator.latency", 0.05, "3") // Tetap kirim baseline latency saat error
		
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Trafik Eror HTTP API Sukses!"))
	})

	fmt.Println("Server API Simulator lengkap berjalan di http://localhost:8080")
	
	// 🌟 5. JALANKAN HTTP LISTEN AND SERVE MENGGUNAKAN MUX YANG SUDAH DIBUNGKUS TRACER
	http.ListenAndServe(":8080", mux)
}

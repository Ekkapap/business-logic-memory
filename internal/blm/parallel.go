package blm

import (
	"os"
	"runtime"
	"strconv"
	"sync"
)

// เจ้าของสั่ง 2026-09-09: งานที่รันเองบนเครื่อง ไม่พึ่ง LLM และไม่ต้องรอใคร ให้ขนานตามทรัพยากรที่มี
// ใช้กับ: ast-grep (ต่อ pattern), อ่าน+parse ไฟล์ตอนสร้างกราฟ, ดึงข้อมูลจาก Qdrant · ไม่ใช้กับ AgentsRoom push (backend ทำหลุดเมื่อขนาน) และตัวติดตั้ง tools (brew/launchd ล็อกกันเอง)

// Workers = จำนวน goroutine สำหรับงาน CPU ในโปรเซสเดียว (ทุกคอร์) · override ด้วย BLM_WORKERS
func Workers() int {
	if v, err := strconv.Atoi(os.Getenv("BLM_WORKERS")); err == nil && v > 0 {
		return v
	}
	return runtime.NumCPU()
}

// ProcWorkers = จำนวนโปรเซสภายนอกที่ spawn พร้อมกัน · ast-grep ขนานภายในตัวเองอยู่แล้ว จึงเอาครึ่งคอร์ (ต่ำสุด 2) ไม่ให้แย่งกันเอง
func ProcWorkers() int {
	if v, err := strconv.Atoi(os.Getenv("BLM_WORKERS")); err == nil && v > 0 {
		return v
	}
	n := runtime.NumCPU() / 2
	if n < 2 {
		n = 2
	}
	return n
}

// forEach รัน fn(i) สำหรับ i ใน [0,n) ด้วย workers goroutine แล้วรอจนครบ · fn ต้องเขียนเฉพาะช่องของตัวเอง (ผู้เรียก merge ตามลำดับทีหลัง ผลจึงคงที่)
func forEach(n, workers int, fn func(i int)) {
	if workers > n {
		workers = n
	}
	if workers <= 1 {
		for i := 0; i < n; i++ {
			fn(i)
		}
		return
	}
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				fn(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		next <- i
	}
	close(next)
	wg.Wait()
}

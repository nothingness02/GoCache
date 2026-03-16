import subprocess
import time
import random
import threading
import os
import signal
import sys

# Configuration
ETCD_CONTAINER = "etcd-service"
PORTS = [8001, 8002, 8003]
# Use absolute path or relative to workspace root
SERVER_CMD = "go run cmd/server/main.go -port {} -etcd localhost:2379"
# Increase requests to ensure it runs long enough for chaos
BENCHMARK_CMD = "go run cmd/benchmark/main.go -c 10 -n 1000 -etcd localhost:2379" 
CHAOS_ROUNDS = 5

procs = {} # port -> subprocess.Popen

def run_command(cmd):
    return subprocess.Popen(cmd, shell=True, preexec_fn=os.setsid)

def start_etcd():
    print("🐳 Starting Etcd (Docker)...")
    subprocess.run(f"docker rm -f {ETCD_CONTAINER}", shell=True, stderr=subprocess.DEVNULL)
    cmd = f"docker run -d --name {ETCD_CONTAINER} -p 2379:2379 -e ALLOW_NONE_AUTHENTICATION=yes bitnami/etcd:latest"
    subprocess.run(cmd, shell=True, check=True)
    time.sleep(3) # Wait for etcd to be ready

def stop_etcd():
    print("🛑 Stopping Etcd...")
    subprocess.run(f"docker rm -f {ETCD_CONTAINER}", shell=True, stderr=subprocess.DEVNULL)

def start_server(port):
    print(f"🚀 Starting server on port {port}...")
    log_file = open(f"server_{port}.log", "w")
    p = subprocess.Popen(SERVER_CMD.format(port), shell=True, stdout=log_file, stderr=log_file, preexec_fn=os.setsid)
    procs[port] = p

def stop_server(port):
    if port in procs:
        print(f"💀 Brutally killing server on port {port}...")
        # Use lsof to find and kill the process listening on the port. 
        # This handles 'go run' child processes better than killpg in some envs.
        # -sTCP:LISTEN ensures we only kill the server, not connected clients.
        cmd = f"kill -9 $(lsof -i :{port} -sTCP:LISTEN -t)"
        subprocess.run(cmd, shell=True, stderr=subprocess.DEVNULL)
        
        # Also clean up the python handle
        p = procs[port]
        try:
            # We already killed it via lsof, but let's ensure the handle is closed
            # and the process group is signaled just in case
            os.killpg(os.getpgid(p.pid), signal.SIGKILL)
            p.wait(timeout=1)
        except Exception:
            pass
        del procs[port]

def chaos_loop(stop_event):
    for i in range(CHAOS_ROUNDS):
        if stop_event.is_set():
            break
        
        time.sleep(3)
        
        if stop_event.is_set():
            break

        # Randomly choose a port to kill
        if not procs:
            continue
            
        target = random.choice(list(procs.keys()))
        stop_server(target)
        
        # Wait a bit
        time.sleep(3)
        
        # Restart
        start_server(target)
        
    print("Chaos loop finished.")

def main():
    stop_event = threading.Event()
    try:
        start_etcd()
        
        # Start all servers
        for p in PORTS:
            start_server(p)
        
        print("⏳ Waiting for servers to register...")
        time.sleep(5) 
        
        # Start Chaos in a separate thread
        chaos_thread = threading.Thread(target=chaos_loop, args=(stop_event,))
        chaos_thread.start()

        # Start Benchmark
        print("📈 Starting Benchmark...")
        # Reduce concurrency and request count slightly for the demo, or match what user wants
        # but 100k requests with sleep might take too long or finish too fast depending on implementation.
        # Adjusted benchmark cmd above to 1000 requests to be quick for a check, 
        # BUT for chaos we want it to run for a while. Let's make it 50000.
        real_bench_cmd = "go run cmd/benchmark/main.go -c 50 -n 1000000 -etcd localhost:2379"
        bench_proc = subprocess.Popen(real_bench_cmd, shell=True)
        
        bench_proc.wait()
        print("Benchmark finished.")
        
        stop_event.set()
        chaos_thread.join()
        
    except KeyboardInterrupt:
        print("\nInterrupted by user.")
    finally:
        print("🧹 Cleaning up...")
        stop_event.set()
        for p in list(procs):
            stop_server(p)
        stop_etcd()

if __name__ == "__main__":
    main()

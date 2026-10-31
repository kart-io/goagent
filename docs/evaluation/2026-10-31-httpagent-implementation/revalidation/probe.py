import http.server, threading, queue, subprocess, os, json, urllib.request, urllib.error, time
from pathlib import Path

events=queue.Queue()
class Callback(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        events.put(json.loads(self.rfile.read(int(self.headers['Content-Length']))))
        self.send_response(204); self.end_headers()
    def log_message(self,*args): pass
server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Callback)
threading.Thread(target=server.serve_forever,daemon=True).start()
env=dict(os.environ,GOAGENT_HTTP_ADDR='127.0.0.1:8099',WEBHOOK_URL=f'http://127.0.0.1:{server.server_port}/events')
log=open('/tmp/httpagent-reverify-callback-server.log','w')
p=subprocess.Popen(['/tmp/goagent-httpagent-reverify'],env=env,stdout=log,stderr=log)
results=[]
def req(path,body,method='POST'):
    r=urllib.request.Request('http://127.0.0.1:8099'+path,data=json.dumps(body).encode(),headers={'Content-Type':'application/json'},method=method)
    try:
        with urllib.request.urlopen(r,timeout=10) as res:return res.status,res.read().decode()
    except urllib.error.HTTPError as e:return e.code,e.read().decode()
try:
    for _ in range(100):
        try: urllib.request.urlopen('http://127.0.0.1:8099/',timeout=.2).close();break
        except OSError:time.sleep(.02)
    for path,body,marker in [('/api/invoke',{'input':'REVERIFY_JSON_3104'},'REVERIFY_JSON_3104'),('/api/webhooks/demo',{'message':'REVERIFY_WEBHOOK_3105'},'REVERIFY_WEBHOOK_3105')]:
        status,text=req(path,body); data=json.loads(text)
        assert status==200 and marker in data['output']['result'],(status,text)
        event=events.get(timeout=5)
        assert event['run_id']==data['run_id'] and event['event_type']=='run.completed' and marker in json.dumps(event,ensure_ascii=False),event
        results.append({'path':path,'status':status,'response':data,'outbound_callback':event})
    for path,body,expected,method in [('/api/invoke',{},400,'POST'),('/api/invoke',{'input':'x'},405,'PUT'),('/api/webhooks/missing',{'message':'x'},404,'POST')]:
        status,text=req(path,body,method);assert status==expected,(path,status,text)
        results.append({'path':path,'method':method,'status':status,'body':text})
    assert events.empty(),'invalid request unexpectedly produced callback'
finally:
    p.terminate(); code=p.wait(timeout=12); server.shutdown();log.close()
    assert code==0,code
Path('/tmp/httpagent-reverify-probe.json').write_text(json.dumps({'checks':results,'graceful_shutdown_exit':code},ensure_ascii=False,indent=2))
print('PASS JSON + inline webhook + actual outbound HTTP callback + 400/405/404 boundaries + graceful shutdown')

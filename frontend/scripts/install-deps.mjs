// Install only when dependency inputs change. Re-running npm ci while Vite is
// using native modules locks their DLLs on Windows and breaks Go hot reloads.
import { createHash } from 'node:crypto';
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';

const root=fileURLToPath(new URL('..',import.meta.url));
const hash=createHash('sha256').update(readFileSync(resolve(root,'package.json'))).update(readFileSync(resolve(root,'package-lock.json'))).digest('hex');
const stamp=resolve(root,'node_modules/.nvs-deps.sha256');
if(existsSync(stamp)&&readFileSync(stamp,'utf8')===hash&&existsSync(resolve(root,'node_modules/.bin/vite'))){
 console.log('Frontend dependencies match the lockfile.');
}else{
 const result=process.platform==='win32'
  ?spawnSync('npm ci',{cwd:root,stdio:'inherit',shell:true})
  :spawnSync('npm',['ci'],{cwd:root,stdio:'inherit'});
 if(result.error)throw result.error;
 if(result.status!==0)process.exit(result.status??1);
 writeFileSync(stamp,hash);
}

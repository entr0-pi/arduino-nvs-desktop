import { useEffect, useRef, useState, type ReactNode } from 'react';

export interface DialogState { title:string; body:ReactNode; confirm:string; danger?:boolean; resolve:(yes:boolean)=>void; backup?:()=>Promise<void>;returnFocus?:HTMLElement|null }
export function Dialog({state}:{state:DialogState|null}) {
  const ref=useRef<HTMLDialogElement>(null);
  const backupLock=useRef(false),[backupBusy,setBackupBusy]=useState(false);
  useEffect(()=>{
    if (!state) return;
    const previous=state.returnFocus??document.activeElement as HTMLElement|null;
    ref.current?.showModal();
    return ()=>{ref.current?.close();queueMicrotask(()=>previous?.focus());};
  },[state]);
  if (!state) return null;
  const finish=(answer:boolean)=>state.resolve(answer);
  return <dialog ref={ref} aria-labelledby="dialog-title" onCancel={e=>{e.preventDefault();if(!backupLock.current)finish(false)}}>
    <div className="dialog-top"><span className="eyebrow">CONFIRM ACTION</span><h2 id="dialog-title">{state.title}</h2></div>
    <div className="dialog-body">{state.body}</div>
    {state.backup&&<button className="backup" disabled={backupBusy} onClick={()=>{if(backupLock.current)return;backupLock.current=true;setBackupBusy(true);void state.backup?.().finally(()=>{backupLock.current=false;setBackupBusy(false)})}}>Save current device values as JSON first</button>}
    <div className="dialog-actions"><button disabled={backupBusy} autoFocus onClick={()=>finish(false)}>Cancel</button><button disabled={backupBusy} className={state.danger?'danger':'primary'} onClick={()=>finish(true)}>{state.confirm}</button></div>
  </dialog>;
}

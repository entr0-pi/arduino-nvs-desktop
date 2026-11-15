import { useCallback, useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { api, toAppError } from './services/wailsApi';
import { editRow, exportValues, loadRows, mergeRead, modified, savedRow, validation, writable } from './services/rows';
import { writeDraft } from './services/writeDraft';
import { ORDER, rowID, type Connection, type LogEntry, type Row, type Schema, type Values } from './types';
import type { DialogState } from './components/Dialog';

export function useManager() {
  const [tab,setTab]=useState('Connect');
  const [connection,setConnection]=useState<Connection>({state:'disconnected'});
  const [ports,setPorts]=useState<string[]>([]),[port,setPort]=useState('');
  const [schema,setSchema]=useState<Schema|null>(null),[fileName,setFileName]=useState('');
  const [rows,setRows]=useState<Row[]>([]),[readOnly,setReadOnly]=useState<Record<string,boolean>>({});
  const [busy,setBusy]=useState(false),[operation,setOperation]=useState('');
  const [notice,setNotice]=useState<{text:string;error?:boolean}|null>(null);
  const [logs,setLogs]=useState<LogEntry[]>([]),[dialog,setDialog]=useState<DialogState|null>(null);
  const [outcomes,setOutcomes]=useState<Record<string,string>>({});
  const [info,setInfo]=useState<Record<string,string|undefined>>({});
  const lock=useRef(false), mounted=useRef(true), logID=useRef(0), rowRef=useRef(rows), connectionRef=useRef(connection);
  const operationTrigger=useRef<HTMLElement|null>(null);
  rowRef.current=rows;connectionRef.current=connection;
  const changeRows=useCallback((fn:(previous:Row[])=>Row[])=>{setRows(previous=>{const next=fn(previous);rowRef.current=next;return next;});},[]);
  const log=useCallback((message:string,level:LogEntry['level']='INFO',source:LogEntry['source']='APP')=>{
    setLogs(items=>[...items,{id:++logID.current,timestamp:new Date().toLocaleTimeString(),level,source,message}].slice(-10000));
  },[]);
  const report=useCallback((error:unknown)=>{
    const e=toAppError(error);setNotice({text:e.detail||e.code,error:true});log(`${e.code}: ${e.detail||'Device refused the operation.'}`,'ERROR','NVS');
    if (e.code==='not_connected') setConnection(c=>({state:'error',port:c.port,error:e.detail}));
    return e;
  },[log]);
  const confirm=useCallback((title:string,body:ReactNode,label:string,danger=false,backup?:()=>Promise<void>)=>new Promise<boolean>(resolve=>{
    setDialog({title,body,confirm:label,danger,backup,returnFocus:operationTrigger.current,resolve:(answer)=>{setDialog(null);resolve(answer);}});
  }),[]);
  const run=useCallback(async(label:string,fn:()=>Promise<void>)=>{
    if (lock.current) return;
    operationTrigger.current=document.activeElement as HTMLElement|null;
    lock.current=true;setBusy(true);setOperation(label);setNotice(null);
    try {await api.busy(true);await fn();}catch(error){report(error);}finally{
      lock.current=false;if(mounted.current){setBusy(false);setOperation('');}void api.busy(false).catch(()=>{});
      queueMicrotask(()=>{if(document.activeElement===document.body)operationTrigger.current?.focus()});
    }
  },[report]);

  const readInternal=useCallback(async(names?:string[])=>{
    const namespaces=names??ORDER.filter(ns=>rowRef.current.some(r=>r.ns===ns));
    for(const ns of namespaces){
      setOperation(`Reading ${ns}…`);
      const results=await api.read(ns);
      changeRows(previous=>previous.map(row=>row.ns===ns?mergeRead(row,results[row.key]??{code:'unknown',error:'No result returned for this key.'}):row));
      if(Object.values(results).some(r=>r.code==='read_only'))setReadOnly(flags=>({...flags,[ns]:true}));
      log(`Read ${ns}: ${Object.values(results).filter(r=>r.value).length} values, ${Object.values(results).filter(r=>r.error).length} key refusals.`,'INFO','NVS');
    }
  },[changeRows,log]);

  const refresh=useCallback(()=>run('Refreshing serial ports…',async()=>{
    const available=await api.ports();setPorts(available);setPort(current=>available.includes(current)?current:(available[0]??''));log(`Found ${available.length} serial port${available.length===1?'':'s'}.`);
  }),[run,log]);

  const importFile=useCallback((path:string,format?:'csv'|'json')=>run('Loading file…',async()=>{
    if(path && !/\.(csv|json)$/i.test(path)) throw {code:'validation',detail:'Choose one .csv schema or .json configuration file.'};
    if(rowRef.current.some(modified) && !await confirm('Replace unsaved changes?',<p>The current draft will be replaced by the new file. Device values for matching keys will be kept.</p>,'Replace draft',true))return;
    const loaded=await api.load(path,format);if(!loaded)return;
    const source=(path.toLowerCase().endsWith('.json')||format==='json')?'JSON':'CSV';
    const label=path?path.split(/[\\/]/).pop()!:(await api.schemaName())||(source==='CSV'?'CSV schema':'JSON configuration');
    changeRows(previous=>loadRows(loaded,previous,source));setSchema(loaded);setFileName(label);setOutcomes({});setTab('Key Pair Management');
    log(`Loaded ${label}: ${loaded.keys.length} keys.`);setNotice({text:`Loaded ${loaded.keys.length} keys. ${connectionRef.current.state==='connected'?'Choose Read device to refresh current values.':'Connect when you are ready to read the ESP32.'}`});
  }),[run,confirm,changeRows,log]);

  const importRef=useRef(importFile);importRef.current=importFile;
  useEffect(()=>{
    mounted.current=true;
    const unsubs=[
      api.on<Connection>('nvs:connection',value=>{setConnection(value);connectionRef.current=value;if(value.state==='error')setReadOnly({});log(`${value.state}${value.port?` · ${value.port}`:''}${value.error?` · ${value.error}`:''}`,value.state==='error'?'ERROR':'INFO','NVS');}),
      api.on<{ns:string;key:string}>('nvs:updated',value=>log(`Acknowledged ${value.ns}/${value.key}.`,'DEBUG','NVS')),
      api.on<{ns:string;key:string}>('nvs:erased',value=>log(`Erased ${value.ns}/${value.key}.`,'DEBUG','NVS')),
      api.on<number>('nvs:schema',count=>log(`Schema contains ${count} keys.`,'DEBUG')),
      api.on<string[]>('files:dropped',paths=>{if(paths.length!==1){report({code:'validation',detail:'Drop one CSV or JSON file at a time.'});return;}void importRef.current(paths[0]);}),
    ];
    void api.info().then(value=>{if(mounted.current&&value)setInfo(value)}).catch(()=>{});
    // Startup requests do not open a device. Ignore work from a development remount.
    let current=true;
    void api.ports().then(available=>{if(current){setPorts(available);setPort(p=>p||available[0]||'');}}).catch(error=>{if(current)report(error)});
    void api.schema().then(loaded=>{if(current&&loaded&&rowRef.current.length===0&&!lock.current){setSchema(loaded);setFileName('Loaded schema');changeRows(previous=>loadRows(loaded,previous,'JSON'));}}).catch(error=>{if(current)report(error)});
    return()=>{current=false;mounted.current=false;unsubs.forEach(off=>off());};
  },[log,report,changeRows]);

  const connect=()=>run('Connecting and waiting for hello…',async()=>{
    setConnection({state:'connecting',port});await api.connect(port);setConnection({state:'connected',port});connectionRef.current={state:'connected',port};setReadOnly({});
    if(rowRef.current.length)await readInternal();
  });
  const disconnect=()=>run('Disconnecting…',async()=>{await api.disconnect();setConnection({state:'disconnected',port:connectionRef.current.port});});
  const read=()=>run('Reading device…',()=>readInternal());
  const readBack=()=>run('Verifying written values…',()=>readInternal(ORDER.filter(ns=>rowRef.current.some(row=>row.ns===ns&&row.saved!==undefined))));
  const exportJSON=()=>run('Exporting configuration…',async()=>{
    if(rowRef.current.some(r=>validation(r)))throw {code:'validation',detail:'Fix the draft validation errors before exporting.'};
    if(!await confirm('Save configuration as JSON?',<p>The file contains WiFi and NTRIP passwords in clear text. Keep it in a trusted location.</p>,'Choose save location'))return;
    await api.export(exportValues(rowRef.current));log('JSON export dialog completed. Canceling leaves files unchanged.');setNotice({text:'JSON export dialog completed.'});
  });

  const write=()=>run('Reviewing changes…',async()=>{
    const selected=rowRef.current.filter(r=>r.selected&&modified(r)&&writable(r,readOnly));
    if(!selected.length || rowRef.current.some(r=>validation(r)))throw {code:'validation',detail:'Select changed, writable rows and fix all field errors.'};
    if(selected.some(r=>r.conflict))throw {code:'validation',detail:'Resolve conflicts before writing: keep the draft or take the device value.'};
    const values:Values={};for(const row of selected){if(row.draft!==undefined)(values[row.ns]??={})[row.key]=row.draft;}
    const target=connectionRef.current.port;
    const canBackup=rowRef.current.some(r=>r.device!==undefined);
    const backup=canBackup?async()=>{
      if(!window.confirm('The backup contains passwords in clear text. Save current device values?'))return;
      try{await api.export(exportValues(rowRef.current,true));log('Device-value backup dialog completed.');}catch(e){report(e);}
    }:undefined;
    if(!await confirm('Write key pairs to ESP32?',<><p>Target <strong>{target}</strong>. The selected keys will be overwritten on the device.</p>{ORDER.filter(ns=>values[ns]).map(ns=><section className="review-group" key={ns}><h3>{ns} <small>{ns==='ntrip'?'Within ~5 s':'At the next boot of the board'}</small></h3>{Object.entries(values[ns]).map(([key,value])=><div className="review-value" key={key}><code>{key}</code><span>{String(value)===''?'""':String(value)}</span></div>)}</section>)}</>,'Write to ESP32',false,backup))return;
    if(connectionRef.current.state!=='connected'||connectionRef.current.port!==target)throw {code:'not_connected',detail:'The target connection changed. Reconnect and review your draft again.'};
    const result=await writeDraft(values,ns=>{setOperation(`Writing ${ns}…`);setOutcomes(previous=>({...previous,[ns]:'Saving…'}));},(ns,written)=>{
      changeRows(previous=>previous.map(row=>row.ns===ns&&written.includes(row.key)?savedRow(row,values[ns][row.key]):row));setOutcomes(previous=>({...previous,[ns]:'Saved'}));log(`Saved ${written.length} keys in ${ns}.`,'INFO','NVS');
    });
    if(result.error){
      const error=result.error,ns=result.stoppedAt!;const written=error.written??[];
      const failedKey=error.key??Object.keys(values[ns]).sort().find(key=>!written.includes(key));
      if(error.code==='read_only')setReadOnly(previous=>({...previous,[ns]:true}));
      if(error.code==='forbidden'&&error.key)changeRows(previous=>previous.map(row=>row.ns===ns&&row.key===error.key?{...row,deviceState:'forbidden',selected:false}:row));
      const stoppedIndex=ORDER.indexOf(ns as typeof ORDER[number]);
      changeRows(previous=>previous.map(row=>{
        if(error.code==='read_only'&&row.ns===ns)row={...row,selected:false};
        if(!Object.hasOwn(values[row.ns]??{},row.key))return row;
        if(row.ns===ns&&written.includes(row.key))return savedRow(row,values[ns][row.key]);
        if(row.ns===ns&&row.key===failedKey)return {...row,error,result:error.code==='no_reply'||error.code==='not_connected'?'Outcome unknown':undefined};
        if(row.ns===ns||ORDER.indexOf(row.ns as typeof ORDER[number])>stoppedIndex)return {...row,result:'Not attempted'};
        return row;
      }));
      setOutcomes(previous=>({...previous,[ns]:'Failed — stopped here'}));report(error);
      return;
    }
    setNotice({text:'Selected key pairs saved. Read back to verify the stored values.'});
  });

  const erase=(row:Row)=>run(`Erasing ${rowID(row)}…`,async()=>{
    if(!await confirm(`Erase ${rowID(row)}?`,<p>This removes the stored key. Erasing it again is safe. The firmware refuses to set it until the next boot seeds its default. Clearing a draft does not erase a key.</p>,'Erase key',true))return;
    try{await api.erase(row.ns,row.key);}catch(e){const error=toAppError(e);if(error.code==='read_only')setReadOnly(previous=>({...previous,[row.ns]:true}));throw error;}
    changeRows(previous=>previous.map(r=>rowID(r)===rowID(row)?{...r,device:undefined,deviceState:'not_found',saved:undefined,error:undefined,result:'Erased',selected:false}:r));log(`Erased ${rowID(row)}.`,'INFO','NVS');
  });
  const edit=(id:string,input:string|boolean)=>{if(!lock.current)changeRows(previous=>previous.map(row=>rowID(row)===id?editRow(row,input):row));};
  const change=(id:string,fn:(row:Row)=>Row)=>{if(!lock.current)changeRows(previous=>previous.map(row=>rowID(row)===id?fn(row):row));};
  const reset=()=>{if(!lock.current)changeRows(previous=>previous.map(row=>({...row,draft:row.device?.value,draftText:undefined,saved:undefined,source:row.device?'ESP32':undefined,selected:false,conflict:false,result:undefined,error:row.deviceState==='forbidden'?row.error:undefined})));};
  const selectAll=(selected:boolean)=>{if(!lock.current)changeRows(previous=>previous.map(row=>({...row,selected:selected&&modified(row)&&writable(row,readOnly)&&!validation(row)&&!row.conflict})));};
  return {tab,setTab,connection,ports,port,setPort,schema,fileName,rows,readOnly,busy,operation,notice,setNotice,logs,clearLogs:()=>setLogs([]),dialog,info,refresh,connect,disconnect,importFile,read,readBack,write,exportJSON,erase,edit,change,reset,selectAll,report,outcomes};
}
export type Manager = ReturnType<typeof useManager>;

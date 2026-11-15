import { useEffect, useRef, useState } from 'react';
import { api } from '../services/wailsApi';
import type { LogEntry } from '../types';

export function LogViewer({entries,onClear,compact=false,onFull}:{entries:LogEntry[];onClear:()=>void;compact?:boolean;onFull?:()=>void}) {
  const [query,setQuery]=useState(''),[level,setLevel]=useState('ALL'),[paused,setPaused]=useState(false),[auto,setAuto]=useState(true),[copyStatus,setCopyStatus]=useState('');
  const held=useRef(entries),end=useRef<HTMLDivElement>(null);
  if(!paused)held.current=entries;
  const visible=(paused?held.current:entries).filter(e=>(level==='ALL'||e.level===level)&&`${e.message} ${e.source}`.toLowerCase().includes(query.toLowerCase())).slice(compact?-100:-10000);
  useEffect(()=>{if(auto&&!paused)end.current?.scrollIntoView?.({block:'nearest'});},[entries,auto,paused]);
  return <section className={`log-panel ${compact?'compact':''}`} aria-label={compact?'Console preview':'App operation logs'}>
    <div className="panel-heading"><div><span className="eyebrow">{compact?'LIVE ACTIVITY':'OPERATION HISTORY'}</span><h2>{compact?'Console':'Application logs'} <span className="count">{entries.length}</span></h2></div><div className="inline-actions">
      <label className="check-label"><input type="checkbox" checked={auto} onChange={e=>setAuto(e.target.checked)}/>Auto-scroll</label>
      {!compact&&<button onClick={()=>setPaused(value=>!value)}>{paused?'Resume':'Pause'}</button>}
      <button onClick={()=>{onClear();held.current=[]}}>Clear</button>{onFull&&<button onClick={onFull}>Open full Logs ↗</button>}
    </div></div>
    {!compact&&<div className="log-tools"><input aria-label="Search logs" placeholder="Search messages…" value={query} onChange={e=>setQuery(e.target.value)}/><select aria-label="Log severity" value={level} onChange={e=>setLevel(e.target.value)}>{['ALL','DEBUG','INFO','WARN','ERROR'].map(x=><option key={x}>{x}</option>)}</select><button onClick={()=>{const text=window.getSelection()?.toString();if(!text){setCopyStatus('Select log text to copy.');return;}void api.copy(text).then(()=>setCopyStatus('Selection copied.')).catch(()=>setCopyStatus('Clipboard unavailable.'));}}>Copy selection</button><span role="status">{paused?'Display paused · incoming messages retained':copyStatus}</span></div>}
    <div className="log-scroll" tabIndex={0}>{!visible.length?<div className="log-empty">No logs yet. Your connection, file and device activity will appear here.</div>:visible.map(entry=><div className="log-entry" key={entry.id}><time>{entry.timestamp}</time><span className={`severity ${entry.level.toLowerCase()}`}>{entry.level}</span><span className="log-source">{entry.source}</span><span>{entry.message}</span></div>)}<div ref={end}/></div>
    <div className="panel-foot"><span className="status-dot"/> App operations only <span className="muted">{compact?'Last 100 entries':'Up to 10,000 entries · local session'}</span></div>
  </section>;
}

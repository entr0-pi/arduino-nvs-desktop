import { ORDER,rowID,type Row } from '../types';
import { hasDraft,modified,validation,writable } from '../services/rows';
import type { Manager } from '../useManager';

const display=(value:unknown)=>value===''?'""':String(value);
export function KeyTable({m}:{m:Manager}) {
  const selected=m.rows.filter(r=>r.selected).length;
  const eligible=m.rows.filter(r=>modified(r)&&writable(r,m.readOnly)&&!validation(r)&&!r.conflict);
  return <section className="table-panel" aria-label="Schema keys">
    <div className="table-toolbar"><label className="check-label"><input type="checkbox" aria-label="Select all changed writable rows" disabled={m.busy||eligible.length===0} checked={eligible.length>0&&eligible.every(r=>r.selected)} onChange={e=>m.selectAll(e.target.checked)}/>Select changed keys</label><span>{selected} selected <span className="divider">/</span> {m.rows.length} keys</span><span className="legend"><i/>Modified <span>○ Unchanged</span></span></div>
    <div className="table-scroll"><table><thead><tr><th aria-label="Select"/><th>Key / type</th><th>Draft value</th><th>Device value <span className="muted">· last read</span></th><th>Source</th><th>Status / result</th><th>Actions</th></tr></thead><tbody>{ORDER.filter(ns=>m.rows.some(r=>r.ns===ns)).map(ns=><Namespace key={ns} ns={ns} m={m}/>)}</tbody></table></div>
    <div className="panel-foot"><span>Draft edits stay here until you confirm a write.</span><span>Saved ≠ Verified · Read back to verify</span></div>
  </section>;
}
function Namespace({ns,m}:{ns:string;m:Manager}) {
  const rows=m.rows.filter(r=>r.ns===ns),readOnly=m.readOnly[ns];
  return <><tr className="namespace"><th colSpan={7}><span className="ns-label">{ns}</span><span>{rows.length} keys</span><span className="namespace-note">{readOnly?'Read-only · fixed by firmware build':ns==='ntrip'?'Changes take effect within ~5 s':'Changes take effect at the next boot'}</span>{m.outcomes[ns]&&<strong>{m.outcomes[ns]}</strong>}</th></tr>{rows.map(row=><KeyRow key={rowID(row)} row={row} m={m}/>)}</>;
}
function KeyRow({row,m}:{row:Row;m:Manager}) {
  const id=rowID(row),invalid=validation(row),changed=modified(row),canWrite=writable(row,m.readOnly),disabled=m.busy||!canWrite;
  const status=row.deviceState==='forbidden'?'Not accessible':m.readOnly[row.ns]?'Read-only':row.conflict?'Conflict':changed?'Modified':'Unchanged';
  const clear=()=>m.change(id,r=>({...r,draft:undefined,draftText:undefined,source:undefined,selected:false,saved:undefined,result:undefined,conflict:false}));
  const revert=()=>m.change(id,r=>({...r,draft:r.device?.value,draftText:undefined,source:r.device?'ESP32':undefined,saved:undefined,result:undefined,conflict:false,error:undefined,selected:false}));
  const device=row.deviceState==='ok'?display(row.device?.value):row.deviceState==='not_found'?'Not stored':row.deviceState==='forbidden'?'Not accessible':row.deviceState==='not_read'?'Not read':'Read error';
  return <tr className={`${changed?'modified':''} ${!canWrite?'restricted':''}`}>
    <td><input type="checkbox" aria-label={`Select ${id}`} checked={row.selected} disabled={disabled||!changed||!!invalid||!!row.conflict} onChange={e=>m.change(id,r=>({...r,selected:e.target.checked}))}/></td>
    <th scope="row"><code>{row.key}</code><span className={`type type-${row.type}`}>{row.type}</span></th>
    <td className="draft-cell">{row.type==='bool'?<label className="boolean-input"><input type="checkbox" aria-label={`Draft ${id}`} checked={row.draft===true} disabled={disabled} onChange={e=>m.edit(id,e.target.checked)}/><span>{hasDraft(row)?display(row.draft):'No proposal'}</span></label>:<input aria-label={`Draft ${id}`} aria-invalid={!!invalid} aria-describedby={invalid?`error-${row.ns}-${row.key}`:undefined} disabled={disabled} type="text" spellCheck={false} autoComplete="off" inputMode={row.type==='int'||row.type==='uint'?'numeric':undefined} value={row.draftText??row.draft?.toString()??''} placeholder="No change proposed" onChange={e=>m.edit(id,e.target.value)}/>}{invalid&&<span className="field-error" id={`error-${row.ns}-${row.key}`}>{invalid}</span>}</td>
    <td className={`device-value ${row.deviceState!=='ok'?'muted':''}`}><span title={device}>{device}</span>{row.deviceState==='not_found'&&<small>Set requires the next boot to seed the default.</small>}</td>
    <td><span className="source">{row.source??'—'}</span></td>
    <td className="row-status"><span className={`row-badge ${status.toLowerCase().replace(' ','-')}`}>{status}</span>{row.result&&<small className={row.result==='Verified'?'success-text':''}>{row.result}</small>}{row.error&&row.deviceState!=='forbidden'&&<small className="error-text">{row.error.detail||row.error.code}</small>}{row.conflict&&<div className="conflict-actions"><button disabled={m.busy} onClick={()=>m.change(id,r=>({...r,conflict:false}))}>Keep draft</button><button disabled={m.busy} onClick={revert}>Take device</button></div>}</td>
    <td><div className="row-actions"><button disabled={disabled||!hasDraft(row)} onClick={clear} title="Clear draft only">Clear</button><button disabled={disabled} onClick={revert}>Revert</button><button className="text-danger" disabled={disabled||m.connection.state!=='connected'} onClick={()=>void m.erase(row)}>Erase</button></div></td>
  </tr>;
}

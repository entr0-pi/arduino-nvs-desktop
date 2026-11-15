import type { KeyResult, Row, Scalar, Schema, Values } from '../types';
import { rowID } from '../types';

export function validation(row: Row): string | undefined {
  if (row.deviceState === 'forbidden') return;
  if (row.type === 'int' || row.type === 'uint') {
    if (row.draftText === undefined && row.draft === undefined) return;
    const text = row.draftText ?? String(row.draft);
    if (!/^-?\d+$/.test(text)) return 'Enter a complete decimal integer.';
    const n = Number(text), min = row.type === 'int' ? -2147483648 : 0, max = row.type === 'int' ? 2147483647 : 4294967295;
    if (!Number.isInteger(n) || n < min || n > max) return `Must be ${min} to ${max}.`;
  }
  if (row.type === 'string' && row.draft !== undefined) {
    if (String(row.draft).includes('\0')) return 'NUL characters are not allowed.';
    if (new TextEncoder().encode(String(row.draft)).length > 256) return 'Maximum 256 UTF-8 bytes.';
  }
}
export const hasDraft = (r: Row) => r.draft !== undefined || r.draftText !== undefined;
export function modified(r: Row): boolean {
  if (!hasDraft(r)) return false;
  if (validation(r)) return true;
  if (r.saved !== undefined && r.draft === r.saved) return false;
  return r.deviceState !== 'ok' || r.draft !== r.device?.value;
}
export const writable = (r: Row, readOnly: Record<string,boolean>) => r.deviceState !== 'forbidden' && !readOnly[r.ns];

export function loadRows(schema: Schema, old: Row[], source: 'CSV'|'JSON'): Row[] {
  const previous = new Map(old.map(r=>[rowID(r),r]));
  const proposals = new Map((schema.proposed ?? []).map(p=>[rowID(p),p.value]));
  return schema.keys.map(spec=>{
    const prior=previous.get(rowID(spec));
    return {...spec,device:prior?.type===spec.type?prior.device:undefined,deviceState:prior?.type===spec.type?prior.deviceState:'not_read',draft:proposals.get(rowID(spec)),source:proposals.has(rowID(spec))?source:undefined,selected:false};
  });
}

export function editRow(row: Row, input: string|boolean): Row {
  const next: Row={...row,source:'Manual',saved:undefined,result:undefined,error:undefined,selected:true};
  if (row.type==='int'||row.type==='uint') { next.draftText=String(input);next.draft=/^-?\d+$/.test(String(input))?Number(input):undefined; }
  else {next.draft=input;next.draftText=undefined;}
  return next;
}

export function mergeRead(row: Row, result: KeyResult): Row {
  if (!result.value) {
    return {...row,device:undefined,deviceState:result.code==='forbidden'?'forbidden':result.code==='not_found'?'not_found':'error',selected:result.code==='forbidden'?false:row.selected,error:{code:result.code??'unknown',detail:result.error??'Read failed.'}};
  }
  const dirty=modified(row);
  const conflict=dirty && row.device!==undefined && row.device.value!==result.value.value;
  const verified=row.saved!==undefined && row.saved===result.value.value;
  const mismatch=row.saved!==undefined && row.saved!==result.value.value;
  return {...row,device:result.value,deviceState:'ok',error:mismatch?{code:'unknown',detail:'Read back differs from the acknowledged write.'}:undefined,
    conflict:conflict||mismatch||row.conflict,
    ...(!dirty && row.saved===undefined ? {draft:result.value.value,draftText:undefined,source:'ESP32' as const}:{}),
    ...(verified?{result:'Verified' as const}:{}),...(mismatch?{saved:undefined}:{}),
  };
}

export function exportValues(rows: Row[], deviceOnly=false): Values {
  const values: Values={};
  for (const row of rows) {
    if (row.deviceState==='forbidden') continue;
    const value=deviceOnly?row.device?.value:(hasDraft(row)?row.draft:row.device?.value);
    if (value===undefined) continue;
    (values[row.ns]??={})[row.key]=value;
  }
  return values;
}

export function savedRow(row: Row, value: Scalar): Row {
  return {...row,saved:value,result:'Saved',selected:false,error:undefined,conflict:false};
}

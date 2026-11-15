import type { AppError, Values } from '../types';
import { ORDER } from '../types';
import { api, toAppError } from './wailsApi';

export async function writeDraft(values: Values, onStart:(ns:string)=>void, onDone:(ns:string,written:string[])=>void): Promise<{stoppedAt?:string;error?:AppError}> {
  for (const ns of ORDER) {
    if (!values[ns] || Object.keys(values[ns]).length===0) continue;
    onStart(ns);
    try { const result=await api.save(ns,values[ns]);onDone(ns,result.written??[]); }
    catch(e) {return {stoppedAt:ns,error:toAppError(e)};}
  }
  return {};
}

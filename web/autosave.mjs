export class Autosave {
  constructor({version, save, onSaved = () => {}, onStatus = () => {}, delay = 650}) {
    Object.assign(this, {version, save, onSaved, onStatus, delay});
    this.pending = null; this.running = null; this.timer = null; this.blocked = false; this.disposed = false;
  }
  get dirty() { return this.pending !== null || this.running !== null; }
  edit(changes) {
    if (this.disposed) return;
    this.pending = {...this.pending, ...changes};
    clearTimeout(this.timer);
    this.onStatus(this.blocked ? 'conflict' : 'pending');
    if (!this.blocked) this.timer = setTimeout(() => this.flush().catch(() => {}), this.delay);
  }
  async flush() {
    clearTimeout(this.timer);
    if (this.blocked) throw new Error('Resolve the conflicting note before continuing.');
    if (this.running) { await this.running; if (this.pending) return this.flush(); return; }
    if (!this.pending || this.disposed) return;
    const changes = this.pending; this.pending = null;
    this.onStatus('saving');
    this.running = (async () => {
      try {
        const note = await this.save({...changes, version:this.version});
        this.version = note.version;
        this.onSaved(note);
        this.onStatus(this.pending ? 'pending' : 'saved');
      } catch (err) {
        this.pending = {...changes, ...this.pending};
        this.blocked = err.status === 409;
        this.onStatus(this.blocked ? 'conflict' : 'error', err);
        if (!this.blocked && !this.disposed && (!err.status || err.status >= 500)) this.timer = setTimeout(() => this.flush().catch(() => {}), 3000);
        throw err;
      } finally { this.running = null; }
    })();
    await this.running;
    if (this.pending) return this.flush();
  }
  dispose() { this.disposed = true; clearTimeout(this.timer); }
}

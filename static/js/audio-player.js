// audio-player.js — прослушивание канала демодулятора.
//
// Звук идёт тем же бинарным потоком, что и спектр (кадры SSTM, тип PCM_I16):
// GET /api/stream/v1/audio?radio_path=…, полоса в заголовке — частота отсчётов.
// Каждый кадр ставится в очередь Web Audio вплотную к предыдущему; небольшой
// запас сглаживает рывки сети, а кадры сверх предела очереди выбрасываются,
// чтобы звук не отставал от эфира.

'use strict';

(function() {
    const DATA_TYPE_PCM_I16 = 0x03;
    // Запас перед первым кадром и после обрыва очереди, с
    const LEAD_S = 0.2;
    // Предел отставания звука от эфира, с
    const MAX_LAG_S = 1.0;

    /**
     * Время начала следующего кадра; -1 — кадр выбросить: после затора сети
     * пришла пачка, и он зазвучал бы позже допустимого.
     * @param {number} now — текущее время звукового контекста, с
     * @param {number} queuedUntil — когда кончается уже поставленный звук, с
     */
    function nextStart(now, queuedUntil, lead, maxLag) {
        if (queuedUntil < now) { return now + lead; }
        if (queuedUntil - now > maxLag) { return -1; }
        return queuedUntil;
    }

    function clamp01(v) {
        return Math.max(0, Math.min(1, Number(v) || 0));
    }

    /**
     * Звуковой контекст создаётся в start(): браузер разрешает звук только
     * по действию пользователя, поэтому start() вызывается из обработчика щелчка.
     * @param {{makeContext?: Function, makeStream?: Function, onStatus?: Function}} [opts]
     */
    function AudioPlayer(opts) {
        const o = opts || {};
        this._makeContext = o.makeContext || function() {
            const Ctx = window.AudioContext || window.webkitAudioContext;
            return new Ctx();
        };
        this._makeStream = o.makeStream || function(so) { return new window.SpectrumStream(so); };
        this._onStatus = o.onStatus || function() {};
        this._volume = 0.8;
        this._ctx = null;
        this._gain = null;
        this._stream = null;
        this._path = null;
        this._queuedUntil = 0;
    }

    AudioPlayer.prototype.isPlaying = function() {
        return this._stream !== null;
    };

    /** Слушать канал радиотракта; повторный вызов с тем же трактом ничего не меняет. */
    AudioPlayer.prototype.start = function(radioPath) {
        if (this._stream && this._path === radioPath) { return; }
        this.stop();
        const ctx = this._makeContext();
        const gain = ctx.createGain();
        gain.gain.value = this._volume;
        gain.connect(ctx.destination);
        this._ctx = ctx;
        this._gain = gain;
        this._path = radioPath;
        this._queuedUntil = 0;

        const self = this;
        const stream = this._makeStream({
            url: '/api/stream/v1/audio?radio_path=' + encodeURIComponent(radioPath),
            onFrame: function(frame) {
                if (self._stream === stream) { self._play(frame); }
            },
            onStatus: function(st) { self._onStatus(st); },
        });
        this._stream = stream;
        stream.start();
        if (ctx.resume) { ctx.resume(); }
    };

    AudioPlayer.prototype.stop = function() {
        const stream = this._stream;
        const ctx = this._ctx;
        this._stream = null;
        this._ctx = null;
        this._gain = null;
        this._path = null;
        if (stream) { stream.stop(); }
        if (ctx && ctx.close) { ctx.close(); }
    };

    /** Громкость 0..1. */
    AudioPlayer.prototype.setVolume = function(v) {
        this._volume = clamp01(v);
        if (this._gain) { this._gain.gain.value = this._volume; }
    };

    AudioPlayer.prototype._play = function(frame) {
        const h = frame.header;
        const samples = frame.values;
        if (!this._ctx || h.dataType !== DATA_TYPE_PCM_I16 || !samples || samples.length === 0 || !(h.spanHz > 0)) {
            return;
        }
        const ctx = this._ctx;
        const t = nextStart(ctx.currentTime, this._queuedUntil, LEAD_S, MAX_LAG_S);
        if (t < 0) { return; }
        const buf = ctx.createBuffer(1, samples.length, h.spanHz);
        buf.getChannelData(0).set(samples);
        const src = ctx.createBufferSource();
        src.buffer = buf;
        src.connect(this._gain);
        src.start(t);
        this._queuedUntil = t + samples.length / h.spanHz;
    };

    if (typeof window !== 'undefined') {
        window.AudioPlayer = AudioPlayer;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { // eslint-disable-line no-undef
            AudioPlayer: AudioPlayer,
            nextStart: nextStart,
            LEAD_S: LEAD_S,
            MAX_LAG_S: MAX_LAG_S,
        };
    }
})();

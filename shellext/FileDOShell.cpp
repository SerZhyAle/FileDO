// FileDOShell.dll - the "File DO.." group in the Windows 11 first-level context
// menu (spec SP-0020). The Store package declares it through
// desktop4:FileExplorerContextMenus and a com:SurrogateServer class
// (msix/AppxManifest.xml), so Explorer reaches it only in the packaged build.
//
// It is code Explorer calls on every right-click, so it does one thing: hand
// the clicked path to filedo.exe. It never opens, reads or classifies a file,
// it loads nothing Explorer does not already have (static CRT, kernel32 and
// ole32 only), and it keeps no state beyond a reference count.
//
// The menu is the classic registration's menu, entry for entry. The table
// below mirrors fdsecMenuItems in cmd/filedo/fdsec_register.go - key, label,
// arguments and separators - and TestShellExtMenuMatchesClassic reads this file
// to hold the two together. Change one, change both, in the same commit.

#include <windows.h>
#include <shobjidl_core.h>
#include <new>

namespace {

// The class Explorer creates. It is the one identity of this handler and is
// declared twice in the package manifest (the verb and the COM class); change
// it and an update leaves Explorer pointing at a class that is gone.
// {7B369FE8-B2BC-41EA-AA71-5427ACAD6E8E}
const CLSID CLSID_FileDOCommand = {0x7b369fe8, 0xb2bc, 0x41ea, {0xaa, 0x71, 0x54, 0x27, 0xac, 0xad, 0x6e, 0x8e}};

const wchar_t kGroupLabel[] = L"File DO..";
const wchar_t kExeName[] = L"filedo.exe";
const wchar_t kIconExe[] = L"filedo_win.exe";
// fdsecPauseFlag and fdsecNoHistoryFlag: every classic command line ends in
// these two, so every command line here does too.
const wchar_t kSuffix[] = L" --pause --no-history";

struct MenuEntry {
    const wchar_t* key;
    const wchar_t* label;
    const wchar_t* args;  // "%1" is where the clicked path goes, as in the registry
    bool separator;       // a line above the entry (ECF_SEPARATORBEFORE there)
    const wchar_t* icon;  // ICON-SET meaning: icons\<icon>.ico beside the DLL (SP-0016 T8)
};

// BEGIN MENU TABLE (read by cmd/filedo/shellext_contract_test.go)
const MenuEntry kEntries[] = {
    {L"10Secure", L"Secure", L"\"%1\" secure", false, L"action.secure"},
    {L"20SecureDel", L"Secure and delete original", L"\"%1\" secure del -y", false, L"action.secure"},
    {L"30SecureWipe", L"Secure and wipe original", L"\"%1\" secure wipe", false, L"action.secure"},
    {L"40SecureRename", L"Secure with a random name", L"\"%1\" secure rename", false, L"action.secure"},
    {L"50Unsecure", L"Unsecure", L"\"%1\" unsecure", true, L"action.unsecure"},
    {L"60UnsecureDel", L"Unsecure and delete container", L"\"%1\" unsecure del -y", false, L"action.unsecure"},
    {L"65UnsecureStart", L"Unsecure and start", L"\"%1\" unsecure start", false, L"action.unsecure"},
    {L"70Wipe", L"Wipe this file", L"file \"%1\" wipe", true, L"action.wipe"},
    {L"80Check", L"Check this file", L"check \"%1\"", false, L"action.verify"},
    {L"90Info", L"Info", L"file \"%1\" info", false, L"app.info"},
};
// END MENU TABLE

const int kEntryCount = sizeof(kEntries) / sizeof(kEntries[0]);

LONG g_objects = 0;
LONG g_locks = 0;
HMODULE g_module = nullptr;

// A growable wide string on the process heap: enough for one command line,
// without pulling a container library into Explorer's address space.
class WBuf {
public:
    WBuf() = default;
    WBuf(const WBuf&) = delete;
    WBuf& operator=(const WBuf&) = delete;
    ~WBuf() { if (p_) HeapFree(GetProcessHeap(), 0, p_); }

    bool Append(const wchar_t* s, size_t n) {
        if (!Reserve(len_ + n + 1)) return false;
        CopyMemory(p_ + len_, s, n * sizeof(wchar_t));
        len_ += n;
        p_[len_] = L'\0';
        return true;
    }
    bool Append(const wchar_t* s) { return Append(s, lstrlenW(s)); }
    wchar_t* Get() { return p_; }
    size_t Len() const { return len_; }
    void Truncate(size_t n) { if (p_ && n < len_) { len_ = n; p_[n] = L'\0'; } }

private:
    bool Reserve(size_t want) {
        if (want <= cap_) return true;
        size_t cap = cap_ ? cap_ : 64;
        while (cap < want) cap *= 2;
        void* q = p_ ? HeapReAlloc(GetProcessHeap(), 0, p_, cap * sizeof(wchar_t))
                     : HeapAlloc(GetProcessHeap(), 0, cap * sizeof(wchar_t));
        if (!q) return false;
        p_ = static_cast<wchar_t*>(q);
        cap_ = cap;
        return true;
    }
    wchar_t* p_ = nullptr;
    size_t len_ = 0;
    size_t cap_ = 0;
};

// ModuleDir is the folder this DLL was loaded from - the package's install
// folder, where filedo.exe and filedo_win.exe sit beside it. Nothing is looked
// up on PATH: the entry runs the exe of the package that declared it.
bool ModuleDir(WBuf& out) {
    DWORD cap = MAX_PATH;
    for (;;) {
        void* raw = HeapAlloc(GetProcessHeap(), 0, cap * sizeof(wchar_t));
        if (!raw) return false;
        wchar_t* buf = static_cast<wchar_t*>(raw);
        DWORD n = GetModuleFileNameW(g_module, buf, cap);
        if (n == 0) { HeapFree(GetProcessHeap(), 0, buf); return false; }
        if (n < cap) {
            DWORD cut = n;
            while (cut > 0 && buf[cut - 1] != L'\\') --cut;
            bool ok = cut > 0 && out.Append(buf, cut);  // keeps the trailing backslash
            HeapFree(GetProcessHeap(), 0, buf);
            return ok;
        }
        HeapFree(GetProcessHeap(), 0, buf);
        if (cap >= 32768) return false;
        cap *= 2;
    }
}

HRESULT DupString(const wchar_t* s, LPWSTR* out) {
    *out = nullptr;
    size_t bytes = (lstrlenW(s) + 1) * sizeof(wchar_t);
    void* p = CoTaskMemAlloc(bytes);
    if (!p) return E_OUTOFMEMORY;
    CopyMemory(p, s, bytes);
    *out = static_cast<LPWSTR>(p);
    return S_OK;
}

// Launch runs one classic command line for one file: "<exe>" <args> --pause
// --no-history, with %1 replaced by the path. A new console, because the
// operation may ask for a password and --pause holds the window open on its
// answer; the file's own folder as the working directory, as a registry verb
// gets it.
void Launch(const wchar_t* dir, const MenuEntry& e, const wchar_t* path) {
    WBuf exe, cmd, cwd;
    if (!exe.Append(dir) || !exe.Append(kExeName)) return;
    if (!cmd.Append(L"\"") || !cmd.Append(exe.Get()) || !cmd.Append(L"\" ")) return;
    for (const wchar_t* a = e.args; *a;) {
        if (a[0] == L'%' && a[1] == L'1') {
            if (!cmd.Append(path)) return;
            a += 2;
        } else {
            if (!cmd.Append(a, 1)) return;
            ++a;
        }
    }
    if (!cmd.Append(kSuffix)) return;

    const wchar_t* workDir = nullptr;
    if (cwd.Append(path)) {
        size_t cut = cwd.Len();
        while (cut > 0 && cwd.Get()[cut - 1] != L'\\') --cut;
        if (cut > 0) {
            // A drive root keeps its backslash ("C:\"); any other folder drops it.
            cwd.Truncate(cut > 3 ? cut - 1 : cut);
            workDir = cwd.Get();
        }
    }

    STARTUPINFOW si = {};
    si.cb = sizeof(si);
    PROCESS_INFORMATION pi = {};
    if (CreateProcessW(exe.Get(), cmd.Get(), nullptr, nullptr, FALSE,
                       CREATE_NEW_CONSOLE | CREATE_UNICODE_ENVIRONMENT, nullptr, workDir, &si, &pi)) {
        CloseHandle(pi.hThread);
        CloseHandle(pi.hProcess);
    }
}

// Command is one node of the menu: the group (index -1), an entry (0..n-1),
// or a separator line. One class for all three keeps the vtable in one place.
class Command final : public IExplorerCommand {
public:
    enum Kind { Group, Entry, Separator };

    Command(Kind kind, int index) : ref_(1), kind_(kind), index_(index) {
        InterlockedIncrement(&g_objects);
    }

    // IUnknown
    IFACEMETHODIMP QueryInterface(REFIID riid, void** ppv) override {
        if (!ppv) return E_POINTER;
        if (riid == IID_IUnknown || riid == IID_IExplorerCommand) {
            *ppv = static_cast<IExplorerCommand*>(this);
            AddRef();
            return S_OK;
        }
        *ppv = nullptr;
        return E_NOINTERFACE;
    }
    IFACEMETHODIMP_(ULONG) AddRef() override { return InterlockedIncrement(&ref_); }
    IFACEMETHODIMP_(ULONG) Release() override {
        LONG n = InterlockedDecrement(&ref_);
        if (n == 0) delete this;
        return n;
    }

    // IExplorerCommand
    IFACEMETHODIMP GetTitle(IShellItemArray*, LPWSTR* name) override {
        if (!name) return E_POINTER;
        *name = nullptr;
        if (kind_ == Separator) return E_NOTIMPL;
        return DupString(kind_ == Group ? kGroupLabel : kEntries[index_].label, name);
    }
    IFACEMETHODIMP GetIcon(IShellItemArray*, LPWSTR* icon) override {
        if (!icon) return E_POINTER;
        *icon = nullptr;
        if (kind_ == Separator) return E_NOTIMPL;
        // The group carries the product mark; an entry carries its meaning's glyph, never the
        // mark (ICON-SET rule 7, ICON-RENDER rule 9). The package always ships icons\.
        WBuf s;
        bool ok = kind_ == Group
            ? ModuleDir(s) && s.Append(kIconExe) && s.Append(L",0")
            : ModuleDir(s) && s.Append(L"icons\\") && s.Append(kEntries[index_].icon) && s.Append(L".ico");
        if (!ok) return E_OUTOFMEMORY;
        return DupString(s.Get(), icon);
    }
    IFACEMETHODIMP GetToolTip(IShellItemArray*, LPWSTR* tip) override {
        if (tip) *tip = nullptr;
        return E_NOTIMPL;
    }
    IFACEMETHODIMP GetCanonicalName(GUID* guid) override {
        if (!guid) return E_POINTER;
        *guid = GUID_NULL;
        return S_OK;
    }
    // Always enabled and never inspected: the classic group is on every file
    // without looking at it, and a state that opened the file would put I/O
    // on every right-click.
    IFACEMETHODIMP GetState(IShellItemArray*, BOOL, EXPCMDSTATE* state) override {
        if (!state) return E_POINTER;
        *state = ECS_ENABLED;
        return S_OK;
    }
    IFACEMETHODIMP Invoke(IShellItemArray* items, IBindCtx*) override {
        if (kind_ != Entry || !items) return S_OK;
        WBuf dir;
        if (!ModuleDir(dir)) return E_OUTOFMEMORY;
        DWORD count = 0;
        if (FAILED(items->GetCount(&count))) return S_OK;
        // One process per selected file, as MultiSelectModel=Player gives the
        // classic entry: each file is its own operation and its own console.
        for (DWORD i = 0; i < count; ++i) {
            IShellItem* item = nullptr;
            if (FAILED(items->GetItemAt(i, &item))) continue;
            LPWSTR path = nullptr;
            if (SUCCEEDED(item->GetDisplayName(SIGDN_FILESYSPATH, &path))) {
                Launch(dir.Get(), kEntries[index_], path);
                CoTaskMemFree(path);
            }
            item->Release();
        }
        return S_OK;
    }
    IFACEMETHODIMP GetFlags(EXPCMDFLAGS* flags) override {
        if (!flags) return E_POINTER;
        *flags = kind_ == Group ? ECF_HASSUBCOMMANDS : kind_ == Separator ? ECF_ISSEPARATOR : ECF_DEFAULT;
        return S_OK;
    }
    IFACEMETHODIMP EnumSubCommands(IEnumExplorerCommand** out) override;

private:
    ~Command() { InterlockedDecrement(&g_objects); }
    LONG ref_;
    Kind kind_;
    int index_;
};

// SubCommands enumerates the group: every entry, with a separator node in
// front of each entry the classic registration draws a line above.
class SubCommands final : public IEnumExplorerCommand {
public:
    SubCommands() : ref_(1), pos_(0) { InterlockedIncrement(&g_objects); }

    IFACEMETHODIMP QueryInterface(REFIID riid, void** ppv) override {
        if (!ppv) return E_POINTER;
        if (riid == IID_IUnknown || riid == IID_IEnumExplorerCommand) {
            *ppv = static_cast<IEnumExplorerCommand*>(this);
            AddRef();
            return S_OK;
        }
        *ppv = nullptr;
        return E_NOINTERFACE;
    }
    IFACEMETHODIMP_(ULONG) AddRef() override { return InterlockedIncrement(&ref_); }
    IFACEMETHODIMP_(ULONG) Release() override {
        LONG n = InterlockedDecrement(&ref_);
        if (n == 0) delete this;
        return n;
    }

    // Positions run over twice the entry count: an even slot is the separator
    // that may stand before entry pos/2, an odd slot is the entry itself.
    IFACEMETHODIMP Next(ULONG want, IExplorerCommand** out, ULONG* got) override {
        if (!out) return E_POINTER;
        ULONG n = 0;
        while (n < want && pos_ < 2 * kEntryCount) {
            int slot = pos_++;
            int idx = slot / 2;
            Command* c = nullptr;
            if (slot % 2 == 0) {
                if (!kEntries[idx].separator || idx == 0) continue;
                c = new (std::nothrow) Command(Command::Separator, idx);
            } else {
                c = new (std::nothrow) Command(Command::Entry, idx);
            }
            if (!c) {
                for (ULONG i = 0; i < n; ++i) { out[i]->Release(); out[i] = nullptr; }
                if (got) *got = 0;
                return E_OUTOFMEMORY;
            }
            out[n++] = c;
        }
        if (got) *got = n;
        return n == want ? S_OK : S_FALSE;
    }
    IFACEMETHODIMP Skip(ULONG count) override {
        IExplorerCommand* c = nullptr;
        for (ULONG i = 0; i < count; ++i) {
            ULONG got = 0;
            if (Next(1, &c, &got) != S_OK) return S_FALSE;
            c->Release();
        }
        return S_OK;
    }
    IFACEMETHODIMP Reset() override { pos_ = 0; return S_OK; }
    IFACEMETHODIMP Clone(IEnumExplorerCommand** out) override {
        if (out) *out = nullptr;
        return E_NOTIMPL;
    }

private:
    ~SubCommands() { InterlockedDecrement(&g_objects); }
    LONG ref_;
    int pos_;
};

IFACEMETHODIMP Command::EnumSubCommands(IEnumExplorerCommand** out) {
    if (!out) return E_POINTER;
    *out = nullptr;
    if (kind_ != Group) return E_NOTIMPL;
    SubCommands* e = new (std::nothrow) SubCommands();
    if (!e) return E_OUTOFMEMORY;
    *out = e;
    return S_OK;
}

class Factory final : public IClassFactory {
public:
    IFACEMETHODIMP QueryInterface(REFIID riid, void** ppv) override {
        if (!ppv) return E_POINTER;
        if (riid == IID_IUnknown || riid == IID_IClassFactory) {
            *ppv = static_cast<IClassFactory*>(this);
            AddRef();
            return S_OK;
        }
        *ppv = nullptr;
        return E_NOINTERFACE;
    }
    // The factory is a static object; its lifetime is the module's.
    IFACEMETHODIMP_(ULONG) AddRef() override { return 2; }
    IFACEMETHODIMP_(ULONG) Release() override { return 1; }

    IFACEMETHODIMP CreateInstance(IUnknown* outer, REFIID riid, void** ppv) override {
        if (!ppv) return E_POINTER;
        *ppv = nullptr;
        if (outer) return CLASS_E_NOAGGREGATION;
        Command* c = new (std::nothrow) Command(Command::Group, -1);
        if (!c) return E_OUTOFMEMORY;
        HRESULT hr = c->QueryInterface(riid, ppv);
        c->Release();
        return hr;
    }
    IFACEMETHODIMP LockServer(BOOL lock) override {
        if (lock) InterlockedIncrement(&g_locks); else InterlockedDecrement(&g_locks);
        return S_OK;
    }
};

Factory g_factory;

}  // namespace

extern "C" BOOL WINAPI DllMain(HINSTANCE instance, DWORD reason, LPVOID) {
    if (reason == DLL_PROCESS_ATTACH) {
        g_module = instance;
        DisableThreadLibraryCalls(instance);
    }
    return TRUE;
}

STDAPI DllGetClassObject(REFCLSID clsid, REFIID riid, void** ppv) {
    if (!ppv) return E_POINTER;
    *ppv = nullptr;
    if (clsid != CLSID_FileDOCommand) return CLASS_E_CLASSNOTAVAILABLE;
    return g_factory.QueryInterface(riid, ppv);
}

STDAPI DllCanUnloadNow() {
    return (g_objects == 0 && g_locks == 0) ? S_OK : S_FALSE;
}

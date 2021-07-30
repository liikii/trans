package main

// SIMPLE SHARE FILES SERVER.
// auth: github.com/liikii
// date: 2021.07.30
// version: 1.0
// ©2021-2051 liikii. All rights reserved.
// 代码版权归作者所有。 保留所有权利。

import "flag"
import "fmt"
import "os"
import "log"
import "net"
import "net/http"
import "io"
import "path/filepath"

// import "context"
import "errors"
import "io/fs"
import "net/url"
import "path"
import "sort"
import "strings"
import "time"
import "mime"
import "strconv"
import "github.com/google/uuid"


var dst string

const sniffLen = 512
const StatusInternalServerError = 500


// curl -i --form "userfile=@haha.txt" http://192.168.1.9:9898/upload?a=%2Fstatic%2F
type Dir string


func FastResp(w http.ResponseWriter, code int) {
    w.Header().Set("Content-Length", "0")
    w.Header().Set("Content-Type", "text/plain; charset=utf-8")
    w.Header().Set("X-Content-Type-Options", "nosniff")
    w.WriteHeader(code)
}



func split_filename(fn string) (string, string) {
    fn_ext := filepath.Ext(fn)
    fn_len :=len(fn)
    ext_len := len(fn_ext)
    return fn[:fn_len-ext_len], fn[fn_len-ext_len:]
}


func mapDirOpenError(originalErr error, name string) error {
    if os.IsNotExist(originalErr) || os.IsPermission(originalErr) {
        return originalErr
    }

    parts := strings.Split(name, string(filepath.Separator))
    for i := range parts {
        if parts[i] == "" {
            continue
        }
        fi, err := os.Stat(strings.Join(parts[:i+1], string(filepath.Separator)))
        if err != nil {
            return originalErr
        }
        if !fi.IsDir() {
            return fs.ErrNotExist
        }
    }
    return originalErr
}


func (d Dir) Open(name string) (http.File, error) {
    if filepath.Separator != '/' && strings.ContainsRune(name, filepath.Separator) {
        return nil, errors.New("http: invalid character in file path")
    }
    dir := string(d)
    if dir == "" {
        dir = "."
    }
    fullName := filepath.Join(dir, filepath.FromSlash(path.Clean("/"+name)))
    f, err := os.Open(fullName)
    if err != nil {
        return nil, mapDirOpenError(err, fullName)
    }
    return f, nil
}

// A FileSystem implements access to a collection of named files.
// The elements in a file path are separated by slash ('/', U+002F)
// characters, regardless of host operating system convention.
// See the FileServer function to convert a FileSystem to a Handler.
//
// This interface predates the fs.FS interface, which can be used instead:
// the FS adapter function converts an fs.FS to a FileSystem.
type FileSystem interface {
    Open(name string) (http.File, error)
}

// A File is returned by a FileSystem's Open method and can be
// served by the FileServer implementation.
//
// The methods should behave the same as those on an *os.File.
// type File interface {
//  io.Closer
//  io.Reader
//  io.Seeker
//  Readdir(count int) ([]fs.FileInfo, error)
//  Stat() (fs.FileInfo, error)
// }

type anyDirs interface {
    len() int
    name(i int) string
    isDir(i int) bool
}

type fileInfoDirs []fs.FileInfo

func (d fileInfoDirs) len() int          { return len(d) }
func (d fileInfoDirs) isDir(i int) bool  { return d[i].IsDir() }
func (d fileInfoDirs) name(i int) string { return d[i].Name() }

type dirEntryDirs []fs.DirEntry

func (d dirEntryDirs) len() int          { return len(d) }
func (d dirEntryDirs) isDir(i int) bool  { return d[i].IsDir() }
func (d dirEntryDirs) name(i int) string { return d[i].Name() }
func (d dirEntryDirs) get(i int) fs.DirEntry  { return d[i] }


var htmlReplacer = strings.NewReplacer(
    "&", "&amp;",
    "<", "&lt;",
    ">", "&gt;",
    // "&#34;" is shorter than "&quot;".
    `"`, "&#34;",
    // "&#39;" is shorter than "&apos;" and apos was not in HTML until HTML5.
    "'", "&#39;",
)

func htmlEscape(s string) string {
    return htmlReplacer.Replace(s)
}


func write_up_part(w http.ResponseWriter, src string)(int64, error){
    sourceFileStat, err := os.Stat(src)
    if err != nil {
            return 0, err
    }

    if !sourceFileStat.Mode().IsRegular() {
            return 0, fmt.Errorf("%s is not a regular file", src)
    }

    source, err := os.Open(src)
    if err != nil {
            return 0, err
    }
    defer source.Close()

    nBytes, err := io.Copy(w, source)
    return nBytes, err
}



func formatFileSize(fileSize int64) (size string) {
   if fileSize < 1024 {
      return fmt.Sprintf("%dB", fileSize)
   } else if fileSize < (1024 * 1024) {
      return fmt.Sprintf("%.2fKB", float64(fileSize)/float64(1024))
   } else if fileSize < (1024 * 1024 * 1024) {
      return fmt.Sprintf("%.2fMB", float64(fileSize)/float64(1024*1024))
   } else if fileSize < (1024 * 1024 * 1024 * 1024) {
      return fmt.Sprintf("%.2fGB", float64(fileSize)/float64(1024*1024*1024))
   } else if fileSize < (1024 * 1024 * 1024 * 1024 * 1024) {
      return fmt.Sprintf("%.2fTB", float64(fileSize)/float64(1024*1024*1024*1024))
   } else { //if fileSize < (1024 * 1024 * 1024 * 1024 * 1024 * 1024)
      return fmt.Sprintf("%.2fEB", float64(fileSize)/float64(1024*1024*1024*1024*1024))
   }
}


func dirList(w http.ResponseWriter, r *http.Request, f http.File) {
    // Prefer to use ReadDir instead of Readdir,
    // because the former doesn't require calling
    // Stat on every entry of a directory on Unix.
    var dirs anyDirs
    var err error
    if d, ok := f.(fs.ReadDirFile); ok {
        var list dirEntryDirs
        list, err = d.ReadDir(-1)
        dirs = list
    } else {
        var list fileInfoDirs
        list, err = f.Readdir(-1)
        dirs = list
    }

    if err != nil {
        // logf(r, "http: error reading directory: %v", err)
        http.Error(w, "Error reading directory", 500)
        return
    }
    sort.Slice(dirs, func(i, j int) bool { return dirs.name(i) < dirs.name(j) })

    w.Header().Set("Content-Type", "text/html; charset=utf-8")

    _, err = write_up_part(w, "static/index_part.html")
    if err != nil {
        // logf(r, "http: error reading directory: %v", err)
        w.Header().Del("Content-Type")
        http.Error(w, "Error reading directory", 500)
        return
    }

    // fmt.Printf("dirs: %+v", dirs)

    fmt.Fprintf(w, `<table><tr><th class="name_c">Name</th><th class="time_c">Last modified</th><th class="size_c">Size</th></tr>`)
    for i, n := 0, dirs.len(); i < n; i++ {
        // fmt.Printf("%T\n", dirs)
        // fmt.Println(dirs.(dirEntryDirs).get(i).Info())

        name := dirs.name(i)

        if dirs.isDir(i) {
            name += "/"
        }

        f_info, _ := dirs.(dirEntryDirs).get(i).Info()
        f_size := formatFileSize(f_info.Size())
        f_time := f_info.ModTime().Format("2006-01-02 15:04:05")



        // fmt.Println(f_info, f_size, f_time)
        // name may contain '?' or '#', which must be escaped to remain
        // part of the URL path, and not indicate the start of a query
        // string or fragment.
        url := url.URL{Path: name}
        f_name :=  fmt.Sprintf("<a class=\"filenameclass\" href=\"%s\">%s</a>\n", url.String(), htmlReplacer.Replace(name))

        tr_cls := "odd"
        if i%2 == 0 {
            tr_cls = "even"
        } 

        // <tr><td class="name_c">%s</td><td class="time_c">%s</td><td class="size_c">%s</td></tr>
        fmt.Fprintf(w, `<tr class="%s"><td class="name_c">%s</td><td class="time_c">%s</td><td class="size_c">%s</td></tr>`, tr_cls, f_name, f_time, f_size)
    }
    fmt.Fprintf(w, "</table>\n")
}


func isSlashRune(r rune) bool { return r == '/' || r == '\\' }


func containsDotDot(v string) bool {
    if !strings.Contains(v, "..") {
        return false
    }
    for _, ent := range strings.FieldsFunc(v, isSlashRune) {
        if ent == ".." {
            return true
        }
    }
    return false
}


func check_is_dir(d string) bool {
    f, err := os.Stat(d)
    if err != nil {
        return false
    }

    fm := f.Mode()
    if !fm.IsDir(){
        return false
    }
    return true
}


func toHTTPError(err error) (msg string, httpStatus int) {
    // if os.IsNotExist(err) {
    //  return "404 page not found", StatusNotFound
    // }
    // if os.IsPermission(err) {
    //  return "403 Forbidden", StatusForbidden
    // }
    // Default:
    // return "500 Internal Server Error", StatusInternalServerError
    return "500 Internal Server Error", 500
}

var StatusMovedPermanently int = 301;
var StatusNotModified int = 304;

func localRedirect(w http.ResponseWriter, r *http.Request, newPath string) {
    if q := r.URL.RawQuery; q != "" {
        newPath += "?" + q
    }
    w.Header().Set("Location", newPath)
    w.WriteHeader(StatusMovedPermanently)
}


func writeNotModified(w http.ResponseWriter) {
    // RFC 7232 section 4.1:
    // a sender SHOULD NOT generate representation metadata other than the
    // above listed fields unless said metadata exists for the purpose of
    // guiding cache updates (e.g., Last-Modified might be useful if the
    // response does not have an ETag field).
    h := w.Header()
    delete(h, "Content-Type")
    delete(h, "Content-Length")
    if h.Get("Etag") != "" {
        delete(h, "Last-Modified")
    }
    w.WriteHeader(StatusNotModified)
}


type condResult int
const (
    condNone condResult = iota
    condTrue
    condFalse
)

func isZeroTime(t time.Time) bool {
    return t.IsZero() || t.Equal(unixEpochTime)
}


var unixEpochTime = time.Unix(0, 0)


func setLastModified(w http.ResponseWriter, modtime time.Time) {
    if !isZeroTime(modtime) {
        w.Header().Set("Last-Modified", modtime.UTC().Format(http.TimeFormat))
    }
}



func checkIfModifiedSince(r *http.Request, modtime time.Time) condResult {
    if r.Method != "GET" && r.Method != "HEAD" {
        return condNone
    }
    ims := r.Header.Get("If-Modified-Since")
    if ims == "" || isZeroTime(modtime) {
        return condNone
    }
    t, err := http.ParseTime(ims)
    if err != nil {
        return condNone
    }
    // The Last-Modified header truncates sub-second precision so
    // the modtime needs to be truncated too.
    modtime = modtime.Truncate(time.Second)
    if modtime.Before(t) || modtime.Equal(t) {
        return condFalse
    }
    return condTrue
}



var errSeeker = errors.New("seeker can't seek")

func get_file_size(content io.ReadSeeker)(int64, error){
    size, err := content.Seek(0, io.SeekEnd)
    if err != nil {
        return 0, errSeeker
    }
    _, err = content.Seek(0, io.SeekStart)
    if err != nil {
        return 0, errSeeker
    }
    return size, nil
}



func serveContent(w http.ResponseWriter, r *http.Request, name string, modtime time.Time, content io.ReadSeeker) {
    setLastModified(w, modtime)

    code := 200

    // If Content-Type isn't set, use the file's extension to find it, but
    // if the Content-Type is unset explicitly, do not sniff the type.
    ctypes, haveType := w.Header()["Content-Type"]
    var ctype string
    if !haveType {
        ctype = mime.TypeByExtension(filepath.Ext(name))
        if ctype == "" {
            // read a chunk to decide between utf-8 text and binary
            var buf [sniffLen]byte
            n, _ := io.ReadFull(content, buf[:])
            ctype = http.DetectContentType(buf[:n])
            _, err := content.Seek(0, io.SeekStart) // rewind to output whole file
            if err != nil {
                http.Error(w, "seeker can't seek", StatusInternalServerError)
                return
            }
        }
        w.Header().Set("Content-Type", ctype)
    } else if len(ctypes) > 0 {
        ctype = ctypes[0]
    }

    size, err := get_file_size(content)
    if err != nil {
        http.Error(w, err.Error(), StatusInternalServerError)
        return
    }

    fmt.Printf("file size: %d bytes\n", size)

    // handle Content-Range header.
    sendSize := size
    var sendContent io.Reader = content
    // if size >= 0 {
    //     ranges, err := parseRange(rangeReq, size)
    //     if err != nil {
    //         if err == errNoOverlap {
    //             w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
    //         }
    //         Error(w, err.Error(), StatusRequestedRangeNotSatisfiable)
    //         return
    //     }
    //     if sumRangesSize(ranges) > size {
    //         // The total number of bytes in all the ranges
    //         // is larger than the size of the file by
    //         // itself, so this is probably an attack, or a
    //         // dumb client. Ignore the range request.
    //         ranges = nil
    //     }
    //     switch {
    //     case len(ranges) == 1:
    //         // RFC 7233, Section 4.1:
    //         // "If a single part is being transferred, the server
    //         // generating the 206 response MUST generate a
    //         // Content-Range header field, describing what range
    //         // of the selected representation is enclosed, and a
    //         // payload consisting of the range.
    //         // ...
    //         // A server MUST NOT generate a multipart response to
    //         // a request for a single range, since a client that
    //         // does not request multiple parts might not support
    //         // multipart responses."
    //         ra := ranges[0]
    //         if _, err := content.Seek(ra.start, io.SeekStart); err != nil {
    //             Error(w, err.Error(), StatusRequestedRangeNotSatisfiable)
    //             return
    //         }
    //         sendSize = ra.length
    //         code = StatusPartialContent
    //         w.Header().Set("Content-Range", ra.contentRange(size))
    //     case len(ranges) > 1:
    //         sendSize = rangesMIMESize(ranges, ctype, size)
    //         code = StatusPartialContent

    //         pr, pw := io.Pipe()
    //         mw := multipart.NewWriter(pw)
    //         w.Header().Set("Content-Type", "multipart/byteranges; boundary="+mw.Boundary())
    //         sendContent = pr
    //         defer pr.Close() // cause writing goroutine to fail and exit if CopyN doesn't finish.
    //         go func() {
    //             for _, ra := range ranges {
    //                 part, err := mw.CreatePart(ra.mimeHeader(ctype, size))
    //                 if err != nil {
    //                     pw.CloseWithError(err)
    //                     return
    //                 }
    //                 if _, err := content.Seek(ra.start, io.SeekStart); err != nil {
    //                     pw.CloseWithError(err)
    //                     return
    //                 }
    //                 if _, err := io.CopyN(part, content, ra.length); err != nil {
    //                     pw.CloseWithError(err)
    //                     return
    //                 }
    //             }
    //             mw.Close()
    //             pw.Close()
    //         }()
    //     }

    //     w.Header().Set("Accept-Ranges", "bytes")
    //     if w.Header().Get("Content-Encoding") == "" {
    //         w.Header().Set("Content-Length", strconv.FormatInt(sendSize, 10))
    //     }
    // }

    w.Header().Set("Accept-Ranges", "bytes")
    w.Header().Set("Content-Length", strconv.FormatInt(sendSize, 10))
    w.WriteHeader(code)

    if r.Method != "HEAD" {
        // io.CopyN(w, sendContent, sendSize)
        b := make([]byte, 4096)
        for {
            n, err := sendContent.Read(b)
            // fmt.Printf("read file bytes: %v, err= %v \n", n, err)
            // fmt.Printf("n = %v err = %v b = %v\n", n, err, b)
            // fmt.Printf("b[:n] = %q\n", b[:n])
            w.Write(b[:n])
            w.(http.Flusher).Flush()
            if err == io.EOF {
                break
            }
        }
    }
}




// name is '/'-separated, not filepath.Separator.
func serveFile(w http.ResponseWriter, r *http.Request, fs FileSystem, name string, redirect bool) {
    // const indexPage = "/index.html"
    fmt.Printf("\nurl path: %v \n", r.URL.Path)
    fmt.Printf("fs: %v\n", fmt.Sprintf("%v", fs))

    const icn_path = "/favicon.ico"
    // redirect .../index.html to .../
    // can't use Redirect() because that would make the path absolute,
    // which would be a problem running under StripPrefix
    if r.URL.Path == icn_path && fmt.Sprintf("%v", fs) != "static" {
        localRedirect(w, r, "/s"+icn_path)
        return
    }


    f, err := fs.Open(name)
    if err != nil {
        msg, code := toHTTPError(err)
        http.Error(w, msg, code)
        return
    }
    defer f.Close()

    d, err := f.Stat()
    if err != nil {
        msg, code := toHTTPError(err)
        http.Error(w, msg, code)
        return
    }



    if redirect {
        // redirect to canonical path: / at end of directory url
        // r.URL.Path always begins with /
        url := r.URL.Path
        if d.IsDir() {
            if url[len(url)-1] != '/' {
                localRedirect(w, r, path.Base(url)+"/")
                return
            }
        } else {
            if url[len(url)-1] == '/' {
                localRedirect(w, r, "../"+path.Base(url))
                return
            }
        }
    }

    if d.IsDir() {

        url := r.URL.Path
        // redirect if the directory name doesn't end in a slash
        if url == "" || url[len(url)-1] != '/' {
            localRedirect(w, r, path.Base(url)+"/")
            return
        }

        // use contents of index.html for directory, if present
        // index := strings.TrimSuffix(name, "/") + indexPage
        // ff, err := fs.Open(index)
        // if err == nil {
        //  defer ff.Close()
        //  dd, err := ff.Stat()
        //  if err == nil {
        //      name = index
        //      d = dd
        //      f = ff
        //  }
        // }
    }

    if checkIfModifiedSince(r, d.ModTime()) == condFalse {
        writeNotModified(w)
        return
    }
    // Still a directory? (we didn't find an index.html file)
    if d.IsDir() {
        if checkIfModifiedSince(r, d.ModTime()) == condFalse {
            writeNotModified(w)
            return
        }
        setLastModified(w, d.ModTime())
        dirList(w, r, f)
        return
    }

    // serveContent will check modification time
    // sizeFunc := func() (int64, error) { return d.Size(), nil }
    // serveContent(w http.ResponseWriter, r *http.Request, name string, modtime time.Time, content io.ReadSeeker)
    serveContent(w, r, d.Name(), d.ModTime(), f)
    // http.ServeContent(w, r, d.Name(), d.ModTime(), f) 
    // http.ServeFile(w, r, ".")
    // http.ServeContent(w, r, d.Name(), d.ModTime(), f)
}


type fileHandler struct {
    root FileSystem
}


func MyFileServer(root FileSystem) http.Handler {
    return &fileHandler{root}
}


func (f *fileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    upath := r.URL.Path
    if !strings.HasPrefix(upath, "/") {
        upath = "/" + upath
        r.URL.Path = upath
    }
    serveFile(w, r, f.root, path.Clean(upath), false)
}


func GetOutboundIP() net.IP {
    conn, err := net.Dial("udp", "8.8.8.8:80")
    if err != nil {
        log.Fatal(err)
    }
    defer conn.Close()
    localAddr := conn.LocalAddr().(*net.UDPAddr)
    return localAddr.IP
}


func check_dir_handler(w http.ResponseWriter, r *http.Request) {
    // 403 405 403 Forbidden
    // 405 Method Not Allowed
    // http.ServeFile(w, r, "static/index.html")
    q := r.URL.Query()
    var c_dir string = q.Get("a")
    if containsDotDot(c_dir){
        FastResp(w, 403)
        return
    }

    path_dir := filepath.Join(dst, c_dir)
    if !check_is_dir(path_dir){
        FastResp(w, 403)
        return
    }
    return
}



func upload(w http.ResponseWriter, r *http.Request) {
    if r.Method == "GET" {
        http.ServeFile(w, r, "static/index.html")
    } else {
        q := r.URL.Query()
        var c_dir string = q.Get("a")
        if containsDotDot(c_dir){
            http.Error(w, "Error current directory", 403)
            return
        }
        var uuid_f string = q.Get("b")
        // fmt.Printf("uuid_f: %s\n", uuid_f)
        uuid_suffix := ""  

        if uuid_f == "1" {
            uuid_n := uuid.New()
            uuid_suffix = "." + strings.ReplaceAll(uuid_n.String(), "-", "")
        } 

        
        path_dir := filepath.Join(dst, c_dir)
        if !check_is_dir(path_dir){
            // fmt.Println("bad dir")
            // localRedirect(w, r, "/s/adfasdf")
            // http.MaxBytesReader(w, r.Body, 8) 
            // r.Body.Close()
            // http.Error(w, "request too large", http.StatusExpectationFailed)
            // ctx := r.Context()
            // _, cancel := context.WithCancel(ctx)
            // cancel()
            // // http.Error(w, "eror", 500)
            // // http.MaxBytesReader(w, r.Body, 8) 
            // // http.Error(w, "request too large", http.StatusExpectationFailed)

            r.Body = http.MaxBytesReader(w, r.Body, 1024) 
            err := r.ParseForm()
            if err != nil {
                fmt.Println("max bytes reader")
                // redirect or set error status code.
                return
            }

            hj, ok := w.(http.Hijacker)
            if !ok {
                http.Error(w, "webserver doesn't support hijacking", http.StatusInternalServerError)
                return
            }
            conn, bufrw, err := hj.Hijack()
            if err != nil {
                http.Error(w, err.Error(), http.StatusInternalServerError)
                return
            }
            // Don't forget to close the connection:
            bufrw.WriteString("HTTP/1.1 500 Internal Server Error\n\n")
            bufrw.Flush()
            conn.Close()
            // // time.Sleep(time.Duration(2)*time.Second)
            return
        }

        // 32mb
        r.ParseMultipartForm(32 << 20)
        // file, handler, err := r.FormFile("file")
        // if err != nil {
        //     fmt.Println("error: 001")
        //     fmt.Println(err)
        //     return
        // }
        // defer file.Close()

        // r.ParseMultipartForm(32 << 20)
        m := r.MultipartForm
        for _, v := range m.File {
            for _, f := range v {
                file, err := f.Open()
                if err != nil {
                    http.Error(w, "eror", 500)
                    fmt.Println("error: 003")
                    fmt.Println(err)
                    return
                }
                defer file.Close()
                // do something with the file data
                // fmt.Println(f.Filename)
                file_name_src := f.Filename
                fmt.Println("file src name: ", file_name_src)
                fn_a, fn_b := split_filename(file_name_src)
                file_name_new := fn_a + uuid_suffix + fn_b
                fmt.Println("file new name: ", file_name_new)
                
                fn_ok := filepath.Join(path_dir, file_name_new)

                f_desc, err := os.OpenFile(fn_ok, os.O_WRONLY|os.O_CREATE, 0666) 
                if err != nil {
                    http.Error(w, "eror", 500)
                    fmt.Println("error: 002")
                    fmt.Println(err)
                    return
                }
                defer f_desc.Close()
                io.Copy(f_desc, file)
                fmt.Printf("\nup: %s --> %s\n", file_name_src, fn_ok)
            }
        }

        // fmt.Fprintf(w, "%v", handler.Header)
        // f, err := os.OpenFile("/tmp/"+handler.Filename, os.O_WRONLY|os.O_CREATE, 0666)  // 此處假設當前目錄下已存在 test 目錄
        // if err != nil {
        //     fmt.Println("error: 002")
        //     fmt.Println(err)
        //     return
        // }
        // defer f.Close()
        // io.Copy(f, file)
    }
}



func main() {

    var dr string
    var pt uint
    var adr string

    flag.StringVar(&dr, "shareddir", ".", "shared Directory.")
    flag.StringVar(&adr, "address", "0.0.0.0", "listen address.")
    flag.UintVar(&pt, "port", 9898, "an listened tcp v4 port.")
    flag.Parse()

    fi, err := os.Stat(dr)
    if err != nil {
        fmt.Println("!!! a directory path need")
        return
    }

    mode := fi.Mode()
    if !mode.IsDir(){
        fmt.Println("!!! a directory path need")
        return
    }

    dst = dr

    f_static, err := os.Stat("static")
    if err != nil {
        fmt.Println("!!!: static directory not exists")
        return
    }

    mode_static := f_static.Mode()
    if !mode_static.IsDir(){
        fmt.Println("!!!: static directory not exists")
        return
    }
    fmt.Println("Shared Directory: ", dr)
    fmt.Println("Listening port: ", pt)

    var pts string

    pts = fmt.Sprintf("%s:%d", adr, pt)

    if adr == "0.0.0.0" || adr == "[::]" {
        fmt.Printf("\thttp://%v:%d/\n\n", GetOutboundIP(), pt)
    } else{
        fmt.Printf("\thttp://%s:%d/\n\n", adr, pt)
    }

    http.Handle("/s/", http.StripPrefix("/s", MyFileServer(http.Dir("static"))))
    http.HandleFunc("/d5033c97b87fec3d5fab7341a3a4c88098a1989256c52e142fe2f0ad757e25978b81cd345e8ed8a3a66d1a32409cfcbb", check_dir_handler)
    http.HandleFunc("/upload", upload)
    http.Handle("/", MyFileServer(http.Dir(dr)))
    // srv := &http.Server{
    //     Addr:           pts,
    //     IdleTimeout:    8 * time.Second,
    //     MaxHeaderBytes: 1 << 20,
    // }
    // log.Fatal(srv.ListenAndServe())
    log.Fatal(http.ListenAndServe(pts, nil))
}

// 2026年1月5日 - golang调用https://github.com/swigger/wechat-ocr/项目的wcocr.dll实现安装了微信的电脑做OCR文字识别
// 此为类的写法
package wcocr_wrap

import (
	"os"
	"encoding/json"
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

type OCRItem struct {
	Text   string  `json:"text"`
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
	Rate   float64 `json:"rate"`
}

type OCRResult struct {
	ErrCode      int       `json:"errcode"`
	ImgPath      string    `json:"imgpath"`
	Width        int       `json:"width"`
	Height       int       `json:"height"`
	OCRResponses []OCRItem `json:"ocr_response"`
}


type WeChatOcr struct {
	wcocrDLL *syscall.LazyDLL
	procWeChatOcr *syscall.LazyProc
	procStopOcr *syscall.LazyProc

	wechatDir string
	ocrExe string

	mu sync.Mutex
	currentCB func(resultJSON string)
	callbackValid bool

	wg sync.WaitGroup
}


func NewWeChatOcr(wcocrDll,WeChatDir,ocrExePath string) (*WeChatOcr,error){
	w:=&WeChatOcr{
		wcocrDLL: syscall.NewLazyDLL(wcocrDll),
		wechatDir: WeChatDir,
		ocrExe: ocrExePath,
	}
	// 装在wcocr.dll后查找对应的函数wechat_ocr和stop_ocr函数地址
	w.procWeChatOcr=w.wcocrDLL.NewProc("wechat_ocr")
	w.procStopOcr=w.wcocrDLL.NewProc("stop_ocr")
	return w,nil
}


// 回调函数
func (w *WeChatOcr) goOcrCallback(cStr *uint8) uintptr {
    w.mu.Lock()
    defer w.mu.Unlock()

    if !w.callbackValid || w.currentCB == nil {
        return 0
    }

    if cStr == nil {
        w.currentCB("{\"errcode\": -1, \"msg\": \"goOcrCallback::null result from dll\"}")
        return 0
    }

    var bytes []byte
    for ptr := uintptr(unsafe.Pointer(cStr)); ; ptr++ {
        b := *(*byte)(unsafe.Pointer(ptr))
        if b == 0 {
            break
        }
        bytes = append(bytes, b)
    }
    w.currentCB(string(bytes))
    return 0
}

func (w *WeChatOcr) DoOCR(imagePath string, cb func(resultJSON string)) error {
	if err := w.procWeChatOcr.Find(); err != nil {
		return fmt.Errorf("DoOCR::load dll wcocr.dll fail or function wechat_ocr not exists, %v", err)
	}

	// 准备宽字符串 (UTF-16) 参数
	wOcrExe, err := syscall.UTF16PtrFromString(w.ocrExe)
	if err != nil {
		return err
	}
	wWechatDir, err := syscall.UTF16PtrFromString(w.wechatDir)
	if err != nil {
		return err
	}
	cImgPath, err := syscall.BytePtrFromString(imagePath)
	if err != nil {
		return err
	}

	w.mu.Lock()
	w.currentCB = cb
	w.callbackValid = true
	w.mu.Unlock()

	callbackPtr := syscall.NewCallback(w.goOcrCallback)
	ret, _, _ := w.procWeChatOcr.Call(uintptr(unsafe.Pointer(wOcrExe)),uintptr(unsafe.Pointer(wWechatDir)),uintptr(unsafe.Pointer(cImgPath)),callbackPtr,)

	if ret == 0 {
		w.mu.Lock()
		w.callbackValid = false
		w.mu.Unlock()
		return fmt.Errorf("DoOCR::wechat_ocr return false,task submit fail")
	}
	return nil
}

func (w *WeChatOcr) WrapOcr(OcrImage string) (string,error){
	var Rt_JSON string

	w.wg.Add(1)
	err := w.DoOCR(OcrImage, func(resultJSON string) {
		Rt_JSON=resultJSON
		defer w.wg.Done()
	})

	if err != nil {
		return "",fmt.Errorf("WrapOcr::Submit OCR Task fail: %v\n", err)
	}

	w.wg.Wait()

	defer func(){
		if err := w.procStopOcr.Find(); err != nil {
			fmt.Printf("WrapOcr::function stop_ocr not find: %v", err)
		}
		w.procStopOcr.Call()
	}()
	return Rt_JSON,nil
}

// 为了进入json package，的占位代码
func parseRs(j string) (OCRResult,error){
	fmt.Printf("收到 OCR 结果（原始 JSON）:\n%s\n", j)
	// 可选：解析为结构体
	var result OCRResult
	if json.Unmarshal([]byte(j), &result) == nil {
		if result.ErrCode == 0 {
			fmt.Printf("识别成功，共 %d 段文字：\n", len(result.OCRResponses))
			for i, item := range result.OCRResponses {
				fmt.Printf("%d: %s (置信度: %.2f%%)\n", i+1, item.Text, item.Rate*100)
			}
		} else {
			fmt.Printf("OCR 失败，errcode: %d\n", result.ErrCode)
		}
	}
	return result,nil
}


// 调用案例代码
func main_test() {
	// wechat 3.xxx的版本
	wcocrDLL:=`F:\github\winshot-1.4.1\build\bin\gowxocr\wcocr.dll`
	wechatDir := `D:\Tools\WeChat.v3.9.5.81\[3.9.5.81]`
	ocrExe := `C:\Users\BlueICE\AppData\Roaming\Tencent\WeChat\XPlugin\Plugins\WeChatOCR\7079\extracted\WeChatOCR.exe`

	var ocrImg string // 文字识别的图像文件路径，完整路径
	if len(os.Args)>=2 {
		ocrImg=os.Args[1]
		wc_ocr,err:=NewWeChatOcr(wcocrDLL,wechatDir,ocrExe)
		if err!=nil{
			fmt.Printf("创建WeChat Ocr实例失败,%v\n",err)
		}else{
			js,err:=wc_ocr.WrapOcr(ocrImg)
			if err!=nil{
				fmt.Printf("文字识别失败,%v\n",err)
			}else{
				fmt.Println(js)
			}
		}
	}else{
		fmt.Printf("错误：请给一个需要进行ocr识别的图像文件\n%s demo.png\n",os.Args[0])
	}


}


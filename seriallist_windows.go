package main

import (
	//"fmt"
	//"github.com/lxn/win"
	"github.com/mattn/go-ole"
	"github.com/mattn/go-ole/oleutil"
	//"github.com/tarm/goserial"
	"log"
	"os"
	"strings"
	//"encoding/binary"
	//"strconv"
	//"syscall"
)

func getList() ([]OsSerialPort, os.SyscallError) {
	return getListViaWmiPnpEntity()
}

func getListViaWmiPnpEntity() ([]OsSerialPort, os.SyscallError) {

	var err os.SyscallError

	//var friendlyName string

	// init COM, oh yeah
	ole.CoInitialize(0)
	defer ole.CoUninitialize()

	unknown, _ := oleutil.CreateObject("WbemScripting.SWbemLocator")
	defer unknown.Release()

	wmi, _ := unknown.QueryInterface(ole.IID_IDispatch)
	defer wmi.Release()

	// service is a SWbemServices
	serviceRaw, _ := oleutil.CallMethod(wmi, "ConnectServer")
	service := serviceRaw.ToIDispatch()
	defer service.Release()

	// result is a SWBemObjectSet
	//pname := syscall.StringToUTF16("SELECT * FROM Win32_PnPEntity where Name like '%" + "COM35" + "%'")
	pname := "SELECT * FROM Win32_PnPEntity WHERE ConfigManagerErrorCode = 0 and Name like '%(COM%'"
	resultRaw, _ := oleutil.CallMethod(service, "ExecQuery", pname)
	result := resultRaw.ToIDispatch()
	defer result.Release()

	countVar, _ := oleutil.GetProperty(result, "Count")
	count := int(countVar.Val)

	list := make([]OsSerialPort, count)

	for i := 0; i < count; i++ {
		// item is a SWbemObject, but really a Win32_Process
		itemRaw, _ := oleutil.CallMethod(result, "ItemIndex", i)
		item := itemRaw.ToIDispatch()
		defer item.Release()

		asString, _ := oleutil.GetProperty(item, "Name")

		//log.println(asString.ToString())

		// get the com port
		s := strings.Split(asString.ToString(), "(COM")[1]
		s = "COM" + s
		s = strings.Split(s, ")")[0]
		list[i].Name = s
		list[i].FriendlyName = asString.ToString()
	}

	for index, element := range list {
		log.Println("index ", index, " element ", element.Name+
			" friendly ", element.FriendlyName)
	}
	return list, err
}

/*
func getListViaOpen() ([]OsSerialPort, os.SyscallError) {

	var err os.SyscallError
	list := make([]OsSerialPort, 100)
	var igood int = 0
	for i := 0; i < 100; i++ {
		prtname := "COM" + strconv.Itoa(i)
		conf := &serial.Config{Name: prtname, Baud: 9600}
		sp, err := serial.OpenPort(conf)
		//log.Println("Just tried to open port", prtname)
		if err == nil {
			//log.Println("Able to open port", prtname)
			list[igood].Name = prtname
			sp.Close()
			list[igood].FriendlyName = getFriendlyName(prtname)
			igood++
		}
	}
	for index, element := range list[:igood] {
		log.Println("index ", index, " element ", element.Name+
			" friendly ", element.FriendlyName)
	}
	return list[:igood], err
}

func getListViaRegistry() ([]OsSerialPort, os.SyscallError) {

	var err os.SyscallError
	var root win.HKEY
	rootpath, _ := syscall.UTF16PtrFromString("HARDWARE\\DEVICEMAP\\SERIALCOMM")
	log.Println(win.RegOpenKeyEx(win.HKEY_LOCAL_MACHINE, rootpath, 0, win.KEY_READ, &root))

	var name_length uint32 = 72
	var key_type uint32
	var lpDataLength uint32 = 72
	var zero_uint uint32 = 0
	name := make([]uint16, 72)
	lpData := make([]byte, 72)

	var retcode int32
	retcode = 0
	for retcode == 0 {
		retcode = win.RegEnumValue(root, zero_uint, &name[0], &name_length, nil, &key_type, &lpData[0], &lpDataLength)
		log.Println("Retcode:", retcode)
		log.Println("syscall name: "+syscall.UTF16ToString(name[:name_length-2])+"---- name_length:", name_length)
		log.Println("syscall lpdata:"+string(lpData[:lpDataLength-2])+"--- lpDataLength:", lpDataLength)
		//log.Println()
		zero_uint++
	}
	win.RegCloseKey(root)
	win.RegOpenKeyEx(win.HKEY_LOCAL_MACHINE, rootpath, 0, win.KEY_READ, &root)

	list := make([]OsSerialPort, zero_uint)
	var i uint32 = 0
	for i = 0; i < zero_uint; i++ {
		win.RegEnumValue(root, i-1, &name[0], &name_length, nil, &key_type, &lpData[0], &lpDataLength)
		//name := string(lpData[:lpDataLength])
		//name = name[:strings.Index(name, '\0')]
		//nameb := []byte(strings.TrimSpace(string(lpData[:lpDataLength])))
		//list[i].Name = string(nameb)
		//list[i].Name = string(name[:strings.Index(name, "\0")])
		//list[i].Name = fmt.Sprintf("%s", string(lpData[:lpDataLength-1]))
		pname := make([]uint16, (lpDataLength-2)/2)
		pname = convertByteArrayToUint16Array(lpData[:lpDataLength-2], lpDataLength-2)
		list[i].Name = syscall.UTF16ToString(pname)
		log.Println("The length of the name is:", len(list[i].Name))
		log.Println("list[i].Name=" + list[i].Name + "---")
		//list[i].FriendlyName = getFriendlyName(list[i].Name)
		list[i].FriendlyName = getFriendlyName("COM34")
	}
	win.RegCloseKey(root)
	return list, err
}

func convertByteArrayToUint16Array(b []byte, mylen uint32) []uint16 {

	log.Println("converting. len:", mylen)
	var i uint32
	ret := make([]uint16, mylen/2)
	for i = 0; i < mylen; i += 2 {
		//ret[i/2] = binary.LittleEndian.Uint16(b[i : i+1])
		ret[i/2] = uint16(b[i]) | uint16(b[i+1])<<8
	}
	return ret
}

func getFriendlyName(portname string) string {

	var friendlyName string

	// init COM, oh yeah
	ole.CoInitialize(0)
	defer ole.CoUninitialize()

	unknown, _ := oleutil.CreateObject("WbemScripting.SWbemLocator")
	defer unknown.Release()

	wmi, _ := unknown.QueryInterface(ole.IID_IDispatch)
	defer wmi.Release()

	// service is a SWbemServices
	serviceRaw, _ := oleutil.CallMethod(wmi, "ConnectServer")
	service := serviceRaw.ToIDispatch()
	defer service.Release()

	// result is a SWBemObjectSet
	//pname := syscall.StringToUTF16("SELECT * FROM Win32_PnPEntity where Name like '%" + "COM35" + "%'")
	pname := "SELECT * FROM Win32_PnPEntity where Name like '%" + portname + "%'"
	resultRaw, _ := oleutil.CallMethod(service, "ExecQuery", pname)
	result := resultRaw.ToIDispatch()
	defer result.Release()

	countVar, _ := oleutil.GetProperty(result, "Count")
	count := int(countVar.Val)

	for i := 0; i < count; i++ {
		// item is a SWbemObject, but really a Win32_Process
		itemRaw, _ := oleutil.CallMethod(result, "ItemIndex", i)
		item := itemRaw.ToIDispatch()
		defer item.Release()

		asString, _ := oleutil.GetProperty(item, "Name")

		println(asString.ToString())
		friendlyName = asString.ToString()
	}

	return friendlyName
}
*/

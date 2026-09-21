package fleet

import "strings"

// ouiVendor is a short table of the hardware-address prefixes a house
// tends to hold, not the registry. An unknown prefix stays unknown, and
// a locally administered address (a phone's private MAC) is "private".
type ouiVendor struct {
	vendor string
	kind   string // the kind the vendor alone implies, or ""
}

var ouiTable = map[string]ouiVendor{}

func init() {
	add := func(vendor, kind string, prefixes ...string) {
		for _, p := range prefixes {
			ouiTable[p] = ouiVendor{vendor, kind}
		}
	}
	add("Raspberry Pi", "computer", "b8:27:eb", "dc:a6:32", "e4:5f:01", "d8:3a:dd", "28:cd:c1", "2c:cf:67")
	add("Espressif", "appliance", "24:0a:c4", "30:ae:a4", "84:0d:8e", "a4:cf:12", "24:6f:28", "3c:71:bf", "8c:aa:b5", "48:3f:da", "bc:dd:c2", "34:94:54", "10:52:1c", "40:91:51", "c8:c9:a3", "e8:68:e7", "7c:9e:bd", "a0:20:a6", "cc:50:e3", "24:62:ab", "ac:0b:fb", "d8:bf:c0", "94:b9:7e", "f4:cf:a2", "b4:e6:2d", "e0:98:06", "58:bf:25", "c4:4f:33", "4c:11:ae")
	add("Tuya", "appliance", "d8:1f:12", "10:d5:61", "7c:f6:66", "68:57:2d", "1c:90:ff", "84:e3:42", "50:8a:06", "98:cd:ac", "24:a1:60", "40:f5:20", "cc:8c:bf", "d4:a6:51", "a4:e5:7c")
	add("Apple", "", "f0:18:98", "a4:83:e7", "3c:06:30", "14:7d:da", "dc:a9:04", "8c:85:90", "f4:5c:89", "28:cf:e9", "9c:f4:8e", "ac:bc:32", "78:4f:43", "b8:e8:56", "a8:66:7f", "d0:03:4b", "f0:d1:a9", "60:f8:1d", "34:36:3b", "bc:d0:74", "4c:57:ca", "6c:96:cf", "e0:b5:5f", "a4:5e:60", "f8:ff:c2", "50:ed:3c", "88:e9:fe", "00:cd:fe", "b8:53:ac", "18:65:90", "38:c9:86", "3c:22:fb", "c0:cc:f8", "70:ea:5a", "f8:4d:89", "d4:61:9d")
	add("Samsung", "", "8c:c8:cd", "00:16:32", "34:23:87", "e8:e8:b7", "50:32:75", "64:1c:ae", "8c:79:67", "b4:79:a7", "c0:97:27", "78:bd:bc", "cc:6e:a4", "d0:c2:4e", "f8:04:2e", "c4:73:1e", "5c:49:7d", "2c:ae:2b", "84:25:db", "bc:14:85", "8c:f5:a3", "c8:1e:e7", "0c:14:20", "24:fd:5b", "d8:c4:6a")
	add("LG", "", "00:1c:62", "20:3d:bd", "64:99:5d", "a8:23:fe", "b4:e6:2a", "c4:9a:02", "58:a2:b5", "3c:cd:93", "cc:2d:8c", "10:68:3f", "74:a7:22", "f8:0c:f3", "c4:36:6c", "38:8c:50")
	add("Sony", "", "04:5d:4b", "00:1d:ba", "fc:f1:52", "30:f9:ed", "84:c7:ea", "10:4f:a8", "ac:9b:0a", "78:c8:81", "40:b8:37", "d8:d4:3c")
	add("Google", "tv", "f4:f5:d8", "54:60:09", "1c:f2:9a", "48:d6:d5", "f8:8f:ca", "30:fd:38", "94:eb:2c", "3c:8d:20", "a4:77:33", "d8:6c:63", "cc:f4:11", "e4:f0:42", "f0:ef:86", "6c:ad:f8", "20:df:b9")
	add("Nest", "appliance", "18:b4:30", "64:16:66")
	add("Amazon", "", "40:b4:cd", "74:c2:46", "f0:27:2d", "68:37:e9", "fc:a1:83", "44:65:0d", "0c:47:c9", "a0:02:dc", "50:dc:e7", "84:d6:d0", "1c:12:b0", "34:d2:70", "38:f7:3d", "78:e1:03", "b0:fc:0d", "ac:63:be", "6c:56:97", "cc:f7:35", "08:7c:39", "4c:ef:c0", "f0:f0:a4", "10:ce:a9", "b0:f7:c4", "18:4f:6c")
	add("Ring", "camera", "34:3e:a4", "54:e0:19", "6c:21:a2")
	add("Sonos", "speaker", "5c:aa:fd", "00:0e:58", "94:9f:3e", "b8:e9:37", "48:a6:b8", "f0:f6:c1", "54:2a:1b", "34:7e:5c", "38:42:0b")
	add("Bose", "speaker", "04:52:c7", "08:df:1f", "00:0c:8a", "2c:41:a1", "60:ab:d2", "c8:7b:23", "4c:87:5d")
	add("Roku", "tv", "b8:3e:59", "d8:31:34", "cc:6d:a0", "08:05:81", "b0:a7:37", "88:de:a9", "20:ef:bd", "ac:ae:19", "10:59:32", "d4:e2:2f", "c8:3a:6b")
	add("Philips Hue", "light", "00:17:88", "ec:b5:fa")
	add("TP-Link", "router", "50:c7:bf", "60:32:b1", "98:da:c4", "b0:be:76", "c0:06:c3", "e8:de:27", "f4:f2:6d", "1c:3b:f3", "54:af:97", "30:de:4b", "5c:e9:31", "68:ff:7b", "a4:2b:b0", "9c:53:22", "b0:95:75", "ac:15:a2", "b4:b0:24", "48:22:54", "28:ee:52", "ec:75:0c", "6c:5a:b0")
	add("Ubiquiti", "router", "24:5a:4c", "74:83:c2", "78:45:58", "80:2a:a8", "f0:9f:c2", "fc:ec:da", "dc:9f:db", "04:18:d6", "68:d7:9a", "e0:63:da", "18:e8:29", "44:d9:e7", "b4:fb:e4", "70:a7:41", "9c:05:d6", "f4:e2:c6", "e4:38:83", "28:70:4e", "d0:21:f9", "60:22:32", "ac:8b:a9")
	add("Netgear", "router", "20:e5:2a", "28:c6:8e", "9c:3d:cf", "a4:2b:8c", "c4:04:15", "e0:46:9a", "08:02:8e", "3c:37:86", "6c:b0:ce", "44:a5:6e", "b0:b9:8a", "a0:04:60", "cc:40:d0", "10:0c:6b", "8c:3b:ad")
	add("ASUS", "router", "04:d4:c4", "1c:87:2c", "2c:56:dc", "30:5a:3a", "50:46:5d", "60:45:cb", "70:4d:7b", "ac:9e:17", "b0:6e:bf", "d4:5d:64", "f0:2f:74", "04:92:26", "3c:7c:3f", "24:4b:fe", "7c:10:c9", "fc:34:97", "a8:5e:45", "08:bf:b8")
	add("Synology", "computer", "00:11:32", "90:09:d0")
	add("QNAP", "computer", "24:5e:be")
	add("Intel", "computer", "00:1b:21", "3c:97:0e", "8c:8d:28", "34:e6:ad", "a4:34:d9", "5c:87:9c", "48:51:b7", "3c:a9:f4", "7c:7a:91", "84:3a:4b", "80:86:f2", "dc:41:a9", "b4:6b:fc", "f8:63:3f", "a0:a4:c5", "e4:b3:18", "2c:6d:c1", "34:13:e8", "8c:f8:c5", "d8:f2:ca", "b0:dc:ef", "ac:74:b1", "90:e8:68", "44:e5:17")
	add("Dell", "computer", "00:14:22", "b8:ac:6f", "d4:be:d9", "18:a9:9b", "54:bf:64", "f8:b1:56", "34:17:eb", "18:66:da", "98:90:96", "00:1a:a0", "14:18:77", "84:2b:2b", "8c:ec:4b", "c8:f7:50", "e4:54:e8", "4c:d9:8f")
	add("Lenovo", "computer", "28:d2:44", "54:e1:ad", "8c:16:45", "e8:6a:64", "98:fa:9b", "50:7b:9d", "c8:5b:76", "00:21:cc", "38:ba:f8", "e0:d4:e8", "f0:d4:15")
	add("Microsoft", "computer", "28:18:78", "7c:1e:52", "98:5f:d3", "c8:3f:26", "58:82:a8", "00:50:f2", "3c:83:75", "6c:5d:3a", "b4:0e:de")
	add("Hyper-V", "computer", "00:15:5d")
	add("VMware", "computer", "00:0c:29", "00:50:56", "00:05:69")
	add("QEMU", "computer", "52:54:00", "bc:24:11")
	add("Xiaomi", "", "28:6c:07", "34:ce:00", "64:09:80", "7c:1c:4e", "78:11:dc", "f8:a2:d6", "04:cf:8c", "50:ec:50", "5c:e5:0c", "3c:bd:3e", "64:90:c1", "8c:be:be", "a4:50:46", "f0:b4:29", "e4:aa:ec", "b0:e2:35", "4c:49:e3", "58:b6:23")
	add("Nintendo", "console", "98:b6:e9", "04:03:d6", "7c:bb:8a", "e8:4e:ce", "cc:9e:00", "a4:38:cc", "58:2f:40", "34:af:2c", "00:1f:32", "dc:68:eb", "98:41:5c", "b8:8a:ec", "e0:0c:7f", "40:f4:07")
	add("Sony Interactive", "console", "00:d9:d1", "70:9e:29", "a8:e3:ee", "fc:0f:e6", "bc:60:a7", "f8:46:1c", "5c:96:56", "78:c8:81", "28:0d:fc", "1c:98:c1")
	add("Brother", "printer", "00:1b:a9", "30:05:5c", "00:80:77", "3c:2a:f4", "a8:93:4a")
	add("HP", "printer", "00:1e:0b", "3c:d9:2b", "9c:8e:99", "10:1f:74", "2c:59:e5", "70:5a:0f", "80:ce:62", "5c:b9:01", "e4:e7:49", "00:68:eb", "c4:65:16", "18:60:24", "a0:8c:fd", "b0:5c:da", "48:ba:4e", "fc:15:b4", "d0:bf:9c", "30:e1:71", "ec:8e:b5")
	add("Canon", "printer", "00:1e:8f", "70:ea:1a", "c4:3e:00", "60:12:8b", "f4:a9:97", "00:bb:c1", "2c:9e:fc", "38:b8:0f", "e8:b6:c2", "54:e8:97")
	add("Epson", "printer", "00:26:ab", "64:eb:8c", "a4:ee:57", "ac:18:26", "00:00:48", "b0:e8:92", "dc:a2:66", "44:d2:44")
	add("Wyze", "camera", "2c:aa:8e", "7c:78:b2", "d0:3f:27")
	add("Reolink", "camera", "ec:71:db")
	add("Hikvision", "camera", "44:19:b6", "54:c4:15", "c0:56:e3", "bc:ad:28", "28:57:be", "4c:bd:8f", "98:8b:0a", "58:03:fb", "18:68:cb", "80:be:af", "a4:14:37")
	add("Ecobee", "appliance", "44:61:32")
	add("Dyson", "appliance", "c8:ff:77")
	add("Logitech", "input", "00:1f:20", "34:88:5d", "c8:e2:65", "44:73:d6")
	add("Garmin", "", "10:c6:fc", "90:f1:57", "00:87:01")
	add("Tesla", "appliance", "4c:fc:aa", "98:ed:5c")
}

// vendorOf looks a hardware address up in the table.
func vendorOf(mac string) (vendor, kind string, private bool) {
	mac = strings.ToLower(mac)
	if len(mac) < 8 {
		return "", "", false
	}
	if v, ok := ouiTable[mac[:8]]; ok {
		return v.vendor, v.kind, false
	}
	// Second hex digit with bit 1 set: locally administered, so a
	// randomized address — a phone or laptop keeping its identity private.
	if c := mac[1]; c == '2' || c == '6' || c == 'a' || c == 'e' {
		return "", "", true
	}
	return "", "", false
}

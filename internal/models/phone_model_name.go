package models

// phoneModelNames maps the identifier an Apple device reports about itself
// (the machine field of uname, such as "iPhone18,2") to the name Apple sells
// it under. An identifier names a hardware variant, so several identifiers
// share one name: the cellular and Wi-Fi iPads, or the regional builds of an
// iPhone.
//
// Every row was checked against two published tables, which agreed on each
// identifier: EveryMac's lists of model identifiers
// (https://everymac.com/devices/by-identifier/all-iphone-model-identifiers.html
// and .../all-ipad-model-identifiers.html) and the community table of Apple
// device identifiers kept in https://gist.github.com/adamawolf/3048717. The
// newest iPhones were also checked against The Apple Wiki and the firmware
// listings at iclarified.com. An identifier that could not be confirmed in two
// places is left out, so it shows as itself rather than as a wrong name.
// Macs are left out: the app is built for iPhone and iPad, and a Mac running
// it does not report a Mac identifier.
var phoneModelNames = map[string]string{
	"iPhone12,1": "iPhone 11",
	"iPhone12,3": "iPhone 11 Pro",
	"iPhone12,5": "iPhone 11 Pro Max",
	"iPhone12,8": "iPhone SE (2nd generation)",
	"iPhone13,1": "iPhone 12 mini",
	"iPhone13,2": "iPhone 12",
	"iPhone13,3": "iPhone 12 Pro",
	"iPhone13,4": "iPhone 12 Pro Max",
	"iPhone14,2": "iPhone 13 Pro",
	"iPhone14,3": "iPhone 13 Pro Max",
	"iPhone14,4": "iPhone 13 mini",
	"iPhone14,5": "iPhone 13",
	"iPhone14,6": "iPhone SE (3rd generation)",
	"iPhone14,7": "iPhone 14",
	"iPhone14,8": "iPhone 14 Plus",
	"iPhone15,2": "iPhone 14 Pro",
	"iPhone15,3": "iPhone 14 Pro Max",
	"iPhone15,4": "iPhone 15",
	"iPhone15,5": "iPhone 15 Plus",
	"iPhone16,1": "iPhone 15 Pro",
	"iPhone16,2": "iPhone 15 Pro Max",
	"iPhone17,1": "iPhone 16 Pro",
	"iPhone17,2": "iPhone 16 Pro Max",
	"iPhone17,3": "iPhone 16",
	"iPhone17,4": "iPhone 16 Plus",
	"iPhone17,5": "iPhone 16e",
	"iPhone18,1": "iPhone 17 Pro",
	"iPhone18,2": "iPhone 17 Pro Max",
	"iPhone18,3": "iPhone 17",
	"iPhone18,4": "iPhone Air",
	"iPhone18,5": "iPhone 17e",
	"iPhone19,2": "iPhone 18 Pro",
	// The 18 Pro Max has two identifiers: the United States build, and the
	// build sold everywhere else.
	"iPhone19,3": "iPhone 18 Pro Max",
	"iPhone19,7": "iPhone 18 Pro Max",

	"iPad7,1":   "iPad Pro 12.9-inch (2nd generation)",
	"iPad7,2":   "iPad Pro 12.9-inch (2nd generation)",
	"iPad7,3":   "iPad Pro 10.5-inch",
	"iPad7,4":   "iPad Pro 10.5-inch",
	"iPad7,5":   "iPad (6th generation)",
	"iPad7,6":   "iPad (6th generation)",
	"iPad7,11":  "iPad (7th generation)",
	"iPad7,12":  "iPad (7th generation)",
	"iPad8,1":   "iPad Pro 11-inch (1st generation)",
	"iPad8,2":   "iPad Pro 11-inch (1st generation)",
	"iPad8,3":   "iPad Pro 11-inch (1st generation)",
	"iPad8,4":   "iPad Pro 11-inch (1st generation)",
	"iPad8,5":   "iPad Pro 12.9-inch (3rd generation)",
	"iPad8,6":   "iPad Pro 12.9-inch (3rd generation)",
	"iPad8,7":   "iPad Pro 12.9-inch (3rd generation)",
	"iPad8,8":   "iPad Pro 12.9-inch (3rd generation)",
	"iPad8,9":   "iPad Pro 11-inch (2nd generation)",
	"iPad8,10":  "iPad Pro 11-inch (2nd generation)",
	"iPad8,11":  "iPad Pro 12.9-inch (4th generation)",
	"iPad8,12":  "iPad Pro 12.9-inch (4th generation)",
	"iPad11,1":  "iPad mini (5th generation)",
	"iPad11,2":  "iPad mini (5th generation)",
	"iPad11,3":  "iPad Air (3rd generation)",
	"iPad11,4":  "iPad Air (3rd generation)",
	"iPad11,6":  "iPad (8th generation)",
	"iPad11,7":  "iPad (8th generation)",
	"iPad12,1":  "iPad (9th generation)",
	"iPad12,2":  "iPad (9th generation)",
	"iPad13,1":  "iPad Air (4th generation)",
	"iPad13,2":  "iPad Air (4th generation)",
	"iPad13,4":  "iPad Pro 11-inch (3rd generation)",
	"iPad13,5":  "iPad Pro 11-inch (3rd generation)",
	"iPad13,6":  "iPad Pro 11-inch (3rd generation)",
	"iPad13,7":  "iPad Pro 11-inch (3rd generation)",
	"iPad13,8":  "iPad Pro 12.9-inch (5th generation)",
	"iPad13,9":  "iPad Pro 12.9-inch (5th generation)",
	"iPad13,10": "iPad Pro 12.9-inch (5th generation)",
	"iPad13,11": "iPad Pro 12.9-inch (5th generation)",
	"iPad13,16": "iPad Air (5th generation)",
	"iPad13,17": "iPad Air (5th generation)",
	"iPad13,18": "iPad (10th generation)",
	"iPad13,19": "iPad (10th generation)",
	"iPad14,1":  "iPad mini (6th generation)",
	"iPad14,2":  "iPad mini (6th generation)",
	"iPad14,3":  "iPad Pro 11-inch (4th generation)",
	"iPad14,4":  "iPad Pro 11-inch (4th generation)",
	"iPad14,5":  "iPad Pro 12.9-inch (6th generation)",
	"iPad14,6":  "iPad Pro 12.9-inch (6th generation)",
	"iPad14,8":  "iPad Air 11-inch (M2)",
	"iPad14,9":  "iPad Air 11-inch (M2)",
	"iPad14,10": "iPad Air 13-inch (M2)",
	"iPad14,11": "iPad Air 13-inch (M2)",
	"iPad15,3":  "iPad Air 11-inch (M3)",
	"iPad15,4":  "iPad Air 11-inch (M3)",
	"iPad15,5":  "iPad Air 13-inch (M3)",
	"iPad15,6":  "iPad Air 13-inch (M3)",
	"iPad15,7":  "iPad (A16)",
	"iPad15,8":  "iPad (A16)",
	"iPad16,1":  "iPad mini (A17 Pro)",
	"iPad16,2":  "iPad mini (A17 Pro)",
	"iPad16,3":  "iPad Pro 11-inch (M4)",
	"iPad16,4":  "iPad Pro 11-inch (M4)",
	"iPad16,5":  "iPad Pro 13-inch (M4)",
	"iPad16,6":  "iPad Pro 13-inch (M4)",
	"iPad16,8":  "iPad Air 11-inch (M4)",
	"iPad16,9":  "iPad Air 11-inch (M4)",
	"iPad16,10": "iPad Air 13-inch (M4)",
	"iPad16,11": "iPad Air 13-inch (M4)",
	"iPad17,1":  "iPad Pro 11-inch (M5)",
	"iPad17,2":  "iPad Pro 11-inch (M5)",
	"iPad17,3":  "iPad Pro 13-inch (M5)",
	"iPad17,4":  "iPad Pro 13-inch (M5)",

	// The simulator reports the architecture of the Mac it runs on instead
	// of a device identifier.
	"x86_64": "Simulator",
	"arm64":  "Simulator",
	"i386":   "Simulator",
}

// PhoneModelName is the name a person knows a device by, such as "iPhone 17
// Pro Max", for the identifier it reported, such as "iPhone18,2". An
// identifier this table does not know is returned as it is: a newer device
// shows its identifier until the table learns it, never a guess.
func PhoneModelName(identifier string) string {
	if name, isKnown := phoneModelNames[identifier]; isKnown {
		return name
	}
	return identifier
}

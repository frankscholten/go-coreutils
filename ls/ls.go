//
// ls.go (go-coreutils) 0.1
// Copyright (C) 2014, The GO-Coreutils Developers.
//
// Written By: Michael Murphy, Abram C. Isola
//
/* TODO:
 * Add (t), sort by modification time, newest first.
 * Add (h, human-readable), with -l, print sizes in human readable format.
 * Add (s, size), print the allocated size of each file, in blocks.
 * Add (S), sort by file size.
 * Add (q, quote-name), enclose entry names in double quotes.
 */
package main

import "bytes"
import "fmt"
import "io"
import "io/ioutil"
import "os"
import "strings"
import "flag"
import "unsafe"
import "runtime"
import "syscall"
import "time"

const ( // Constant variables used throughout the program.
	TERMINAL_INFO    = 0x5413         // Used in the getTerminalWidth function
	EXECUTABLE       = 0111           // File executable bit
	SYMLINK          = os.ModeSymlink // Symlink bit
	CYAN_SYMLINK     = "\x1b[36;1m"   // Cyan terminal color
	BLUE_DIR         = "\x1b[34;1m"   // Blue terminal color
	GREEN_EXECUTABLE = "\x1b[32;1m"   // Green terminal color
	RESET            = "\x1b[0m"      // Reset terminal color
	SPACING          = 1              // Spacing between columns
	DATE_FORMAT      = "Jan _2 15:04" // Format date
	DATE_YEAR_FORMAT = "Jan _2  2006" // If the file is from a previous year

	help_text string = `
    Usage: ls [OPTION]...
    
    list files and directories in working directory

        --help        display this help and exit
        --version     output version information and exit

        -a    include hidden files and directories
        
        -d, -directory
              list only directories and not their contents

        -l    use a long listing format
        
        -n, -numeric-uid-gid
              list numeric uid/gid's instead of names

        -r, -reverse
              reverse order while sorting
              
        -1    list in a single column
`
	version_text = `
    ls (go-coreutils) 0.1

    Copyright (C) 2014, The GO-Coreutils Developers.
    This program comes with ABSOLUTELY NO WARRANTY; for details see
    LICENSE. This is free software, and you are welcome to redistribute 
    it under certain conditions in LICENSE.
`
)

var ( // Default flags and variables.
	help            = flag.Bool("help", false, "display help information")
	version         = flag.Bool("version", false, "display version information")
	showHidden      = flag.Bool("a", false, "list hidden files and directories")
	dirOnly         = flag.Bool("d", false, "list only directories and not their contents")
	dirOnlyLong     = flag.Bool("directory", false, "list only directories and not their contents")
	longMode        = flag.Bool("l", false, "use a long listing format")
	numericIDs      = flag.Bool("n", false, "list numeric uid/gid's instead of names.")
	numericIDsLong  = flag.Bool("numeric-uid-gid", false, "list numeric uid/gid's instead of names.")
	reversed        = flag.Bool("r", false, "reverse order while sorting")
	reversedLong    = flag.Bool("reverse", false, "reverse order while sorting")
	singleColumn    = flag.Bool("1", false, "list files by one column")
	printOneLine    = true                    // list in a single columnlist in a single columnets whether or not to print on one row.
	terminalWidth   = int(getTerminalWidth()) // Grabs information on the current terminal width.
	maxIDLength     = 0                       // Statistics for the longest id name length.
	maxSizeLength   = 0                       // Statistics for the longest file size length.
	totalCharLength = 0                       // Statistics for the total number of characters.
	maxCharLength   = 0                       // Statistics for maximum file name length.
	fileList        = make([]os.FileInfo, 0)  // A list of all files being processed
	fileLengthList  = make([]int, 0)          // A list of file character lengths
	fileModeList    = make([]string, 0)       // A list of file mode strings
	fileUserList    = make([]string, 0)       // A list of user values
	fileGroupList   = make([]string, 0)       // A list of group values
	fileModDateList = make([]string, 0)       // A list of file modication times.
	fileSizeList    = make([]int64, 0)        // A list of file sizes.
)

// Check initial state of flags.
func processFlags() {
	flag.Parse()
	if *help {
		fmt.Println(help_text)
		os.Exit(0)
	}
	if *version {
		fmt.Println(version_text)
		os.Exit(0)
	}
	if *reversedLong {
		*reversed = true
	}
	if *numericIDsLong {
		*numericIDs = true
	}
}

// Stores information regarding the terminal size.
type termsize struct {
	Row, Col, Xpixel, Ypixel uint16
}

// Obtains the current width of the terminal.
func getTerminalWidth() uint {
	ws := &termsize{}
	retCode, _, errno := syscall.Syscall(syscall.SYS_IOCTL,
		uintptr(syscall.Stdin),
		uintptr(TERMINAL_INFO),
		uintptr(unsafe.Pointer(ws)))
	if int(retCode) == -1 {
		panic(errno)
	}
	return uint(ws.Col)
}

// Displays error messages
func errorChecker(err *error, message string) {
	if *err != nil {
		fmt.Print(message)
		os.Exit(0)
	}
}

// If there is no argument, set the directory path to the current working directory
func getPath() string {
	if flag.NArg() < 1 {
		path, err := os.Getwd()
		errorChecker(&err, "ls: Could not obtain the current working directory.\n")
		return path
	} else {
		if strings.HasPrefix(flag.Arg(0), ".") {
			return flag.Arg(0)
		} else {
			return flag.Arg(0) + "/"
		}
	}
}

// Checks if the file can be shown
func fileIsNotHidden(file string) bool {
	if strings.HasPrefix(file, ".") {
		return false
	} else {
		return true
	}
}

// Scans the directory and returns a list of the contents. If the directory
// does not exist, an error is printed and the program exits.
func scanDirectory() {
	// If enabled, only print the directory -- not it's contents.
	if *dirOnly {
		directory, err := os.Stat(getPath())
		errorChecker(&err, "ls: "+getPath()+" - No such file or directory.\n")
		fileList = append(fileList, directory)
	} else {
		directory, err := ioutil.ReadDir(getPath())
		errorChecker(&err, "ls: "+getPath()+" - No such file or directory.\n")
		if *showHidden {
			fileList = directory
		} else {
			for _, file := range directory {
				if fileIsNotHidden(file.Name()) {
					fileList = append(fileList, file)
				}
			}
		}
	}
}

// Obtain lists of file information
func getFileStats() {
	// Channels for the goroutines to check when they finish.
	lengthDone := make(chan bool)
	oneLineCheck := make(chan bool)
	maxCharLengthCheck := make(chan bool)

	// The goroutines used to grab all file statistics in parallel for a slight performance boost.
	go getFileLengthList(lengthDone)
	go getMaxCharacterLength(maxCharLengthCheck)
	go printOneLineCheck(oneLineCheck)

	// If longMode is enabled
	if *longMode {
		modeDone := make(chan bool)
		modDateDone := make(chan bool)
		sizeDone := make(chan bool)
		userDone := make(chan bool)
		groupDone := make(chan bool)
		countDone := make(chan bool)

		go getModeTypeList(modeDone)
		go getModDateList(modDateDone)
		go getFileSize(sizeDone)
		go getUserList(userDone)
		go getGroupList(groupDone)

		<-userDone
		<-groupDone
		<-sizeDone
		go countMaxSizeLength(countDone)
		<-modeDone
		<-modDateDone
		<-countDone
	}

	// Synchronize goroutines with main
	<-lengthDone
	<-maxCharLengthCheck
	<-oneLineCheck
}

// Obtain file statistics
func openSymlink(file string) os.FileInfo {
	var fi os.FileInfo
	if !strings.HasPrefix(file, "/") {
		fi, _ = os.Stat(getPath() + file)
	} else {
		fi, _ = os.Stat(file)
	}
	return fi
}

// Resolve the symbolic links
func readLink(file string) string {
	sympath, err := os.Readlink(getPath() + file)
	if err == nil {
		return sympath
	} else {
		return "broken link"
	}
}

/* If the file is a symbolic link, print it in cyan; a directory, blue; an executable file,
 * green; else print the file in white. */
func colorizer(file os.FileInfo) string {
	switch {
	case file.Mode()&SYMLINK != 0:
		return CYAN_SYMLINK + file.Name()
	case file.IsDir():
		return BLUE_DIR + file.Name()
	case file.Mode()&EXECUTABLE != 0:
		return GREEN_EXECUTABLE + file.Name()
	default:
		return RESET + file.Name()
	}
}

// Checks if the date of the file is from a prior year, and if so print the year, else print
// only the hour and minute.
func dateFormatCheck(fileModTime time.Time) string {
	if fileModTime.Year() != time.Now().Year() {
		return fileModTime.Format(DATE_YEAR_FORMAT)
	} else {
		return fileModTime.Format(DATE_FORMAT)
	}
}

// Opens the passwd file and returns a buffer of it's contents.
func bufferUsers() *bytes.Buffer {
	buffer := bytes.NewBuffer(nil)
	cached, err := os.Open("/etc/passwd")
	if err != nil {
		fmt.Println("Error: passwd file does not exist.")
		os.Exit(0)
	}
	io.Copy(buffer, cached)
	return buffer
}

// Opens the group file and returns a buffer of it's contents.
func bufferGroups() *bytes.Buffer {
	buffer := bytes.NewBuffer(nil)
	cached, err := os.Open("/etc/group")
	if err != nil {
		fmt.Println("Error: group file does not exist.")
		os.Exit(0)
	}
	io.Copy(buffer, cached)
	return buffer
}

// Converts a bytes buffer into a newline-separated string array.
func bufferToStringArray(buffer *bytes.Buffer) []string {
	return strings.Split(buffer.String(), "\n")
}

// Returns a colon separated string array for use in parsing /etc/group and /etc/user
func parseLine(line string) []string {
	return strings.Split(line, ":")
}

// Returns user id
func getUID(file os.FileInfo) string {
	return fmt.Sprintf("%d", file.Sys().(*syscall.Stat_t).Uid)
}

// Returns group id
func getGID(file os.FileInfo) string {
	return fmt.Sprintf("%d", file.Sys().(*syscall.Stat_t).Gid)
}

// Obtains a list of formatted file modification dates.
func getModDateList(done chan bool) {
	for _, file := range fileList {
		fileModDateList = append(fileModDateList, dateFormatCheck(file.ModTime()))
	}
	done <- true
}

// Returns the username associated to a user ID
func lookupUserID(uid string, userStringArray []string) string {
	for _, line := range userStringArray {
		values := parseLine(line)
		if len(values) > 2 {
			if values[2] == uid {
				return values[0]
			}
		}
	}
	return uid
}

// Returns the groupname associated to a group ID
func lookupGroupID(gid string, groupStringArray []string) string {
	for _, line := range groupStringArray {
		values := parseLine(line)
		if len(values) > 2 {
			if values[2] == gid {
				return values[0]
			}
		}
	}
	return gid
}

// Obtains a list of file sizes.
func getFileSize(done chan bool) {
	for _, file := range fileList {
		fileSizeList = append(fileSizeList, file.Size())
	}
	done <- true
}

// Obtains a list of user names
func getUserList(done chan bool) {
	userBuffer := bufferToStringArray(bufferUsers())
	var uid string
	for _, file := range fileList {
		if *numericIDs {
			uid = getUID(file)
		} else {
			uid = lookupUserID(getUID(file), userBuffer)
		}
		fileUserList = append(fileUserList, uid)
	}
	done <- true
}

// Obtains a list of group names
func getGroupList(done chan bool) {
	groupBuffer := bufferToStringArray(bufferGroups())
	var gid string
	for _, file := range fileList {
		if *numericIDs {
			gid = getGID(file)
		} else {
			gid = lookupGroupID(getGID(file), groupBuffer)
		}
		fileGroupList = append(fileGroupList, gid)
	}
	done <- true
}

// Obtains a list of file character lengths.
func getFileLengthList(done chan bool) {
	for _, file := range fileList {
		fileLengthList = append(fileLengthList, len(file.Name()))
	}
	done <- true
}

// Obtains the mode type of the file in string format.
func getModeType(file os.FileInfo) string {
	return file.Mode().String()
}

// Obtains a list of mode types in string format.
func getModeTypeList(done chan bool) {
	for _, file := range fileList {
		fileModeList = append(fileModeList, file.Mode().String())
	}
	done <- true
}

// The spacer function will add spaces to the end of each file name so that they line up
// correctly when printing in the printTopToBottom function.
func spacer(name string, charLength int) string {
	return string(name + strings.Repeat(" ", maxCharLength-charLength+SPACING))
}

// Obtains a list of colorized and spaced names for printTopToBottom.
func getColorizedList() []string {
	colorizedList := make([]string, 0)
	for index, file := range fileList { // Preprocesses the file list for printing by adding spaces.
		colorizedList = append(colorizedList, spacer(colorizer(file), fileLengthList[index]))
	}
	return colorizedList
}

// Determines the character length of the longest file name.
func getMaxCharacterLength(done chan bool) {
	for _, file := range fileList {
		if len(file.Name()) > maxCharLength {
			maxCharLength = len(file.Name())
		}
	}
	done <- true
}

// Determines the max character length of file size and user/group names/ids.
func countMaxSizeLength(done chan bool) {
	for index, file := range fileList {
		countSizeLength(file.Size())
		countIDLength(&fileUserList[index], &fileGroupList[index])
	}
	done <- true
}

// Determines the maximum id name length for printing with long mode.
func countIDLength(uid, gid *string) {
	if len(*uid) > maxIDLength {
		maxIDLength = len(*uid)
	}
	if len(*gid) > maxIDLength {
		maxIDLength = len(*gid)
	}
}

// Determines the maximum size name length for printing with long mode.
func countSizeLength(fileSize int64) {
	length := len(fmt.Sprintf("%d", fileSize))
	if length > maxSizeLength {
		maxSizeLength = length
	}
}

// Determines if we can print on one line.
func printOneLineCheck(done chan bool) {
	for _, file := range fileList {
		if totalCharLength <= terminalWidth {
			totalCharLength += len(file.Name()) + 2 // The additional 2 is for spacing.
		} else {
			printOneLine = false
			done <- true
		}
	}
	printOneLine = true
	done <- true
}

// Returns the printing layout for long mode.
func getLongModeLayout() string {
	ownershipLayout := fmt.Sprintf("%d", maxIDLength)
	sizeLayout := fmt.Sprintf("%d", maxSizeLength)

	return "%11s %-" + ownershipLayout + "s %-" + ownershipLayout + "s %" + sizeLayout + "d %12s %s\n"
}

// Prints a single colorized file in long mode. If the file is a symbolic link, it will also print
// the location that the symbolic link resolves to and what type of file it is.
func printLongModeFile(file os.FileInfo, index *int) {
	printingLayout := getLongModeLayout()
	var fileName string

	if file.Mode()&SYMLINK != 0 {
		symPath := readLink(file.Name())
		fileName = colorizer(file) + RESET + " -> " + colorizer(openSymlink(symPath))
	} else {
		fileName = colorizer(file)
	}

	fmt.Printf(printingLayout, fileModeList[*index], fileUserList[*index],
		fileGroupList[*index], fileSizeList[*index], fileModDateList[*index], fileName+RESET)
}

// Prints files in long mode
func longModePrinter() {
	// Print number of files in the directory
	fmt.Println("total:", len(fileList))

	if *reversed {
		for index := len(fileList) - 1; index >= 0; index-- {
			file := fileList[index]
			printLongModeFile(file, &index)
		}
	} else {
		for index, file := range fileList {
			printLongModeFile(file, &index)
		}
	}
}

// Prints all files in one line
func oneLinePrinter() {
	if *reversed {
		for index := len(fileList) - 1; index >= 0; index-- {
			fmt.Print(colorizer(fileList[index]), "  ")
		}
	} else {
		for _, file := range fileList {
			fmt.Print(colorizer(file), "  ") // Print the file plus additional spacing
		}
	}
	fmt.Println(RESET)
}

// Prints all files in one column
func singleColumnPrinter() {
	if *reversed {
		for index := len(fileList) - 1; index >= 0; index-- {
			fmt.Println(colorizer(fileList[index]))
		}
	} else {
		for _, file := range fileList {
			fmt.Println(colorizer(file))
		}
	}
	fmt.Print(RESET)
}

// Increases index count in printTopToBottom based on current position.
// The index must take into account the fact that the last row needs files to print as well.
// After we are certain that the last row is happy, we can then start increasing index count by
// the number of rows minus one.
func indexCounter(currentIndex, column, lastRowCount, numOfRows *int) int {
	if *column >= *lastRowCount+1 {
		return *currentIndex + *numOfRows - 1
	} else {
		return *currentIndex + *numOfRows
	}
}

// Returns the printing order based on the number of files and maximum column width.
func getTopToBottomOrder(maxColumns, numOfFiles *int, numOfRows, lastRowCount int) []int {
	var currentRow, currentIndex int = 1, 0
	printOrder := make([]int, 0)

	// TODO: Parallelize this process by creating as many goroutine workers
	// as columns and appending each completed job slice in order.
	for index := 0; index < *numOfFiles; index++ {
		if currentRow < numOfRows {
			for column := 1; column < *maxColumns; column++ {
				printOrder = append(printOrder, currentIndex)
				currentIndex = indexCounter(&currentIndex, &column, &lastRowCount, &numOfRows)
				index++
			}
			printOrder = append(printOrder, currentIndex)
			currentRow++
			currentIndex = currentRow - 1
		} else {
			for column := 1; column <= lastRowCount; column++ {
				printOrder = append(printOrder, currentIndex)
				currentIndex += numOfRows
				index++
			}
		}
	}

	return printOrder
}

// Returns the maximum number of columns to print
func getMaxColumns(done chan int) {
	done <- terminalWidth / (maxCharLength + SPACING)
}

// Returns the number of files to print
func getNumOfFiles(done chan int) {
	done <- len(fileList)
}

// Returns the number of files on the last row
func getLastRowCount(numOfFiles, maxColumns *int, done chan int) {
	done <- *numOfFiles % *maxColumns
}

// Returns the number of rows to print
func countRows(maxColumns, numOfFiles *int, done chan int) {
	done <- *numOfFiles / *maxColumns + 1
}

// Returns various required data for printing from top to bottom.
func getPrintTopToBottomData() (int, int, int, []int) {
	maxColumnsChan := make(chan int)
	numOfFilesChan := make(chan int)
	lastRowCountChan := make(chan int)
	numOfRowsChan := make(chan int)

	go getMaxColumns(maxColumnsChan)
	go getNumOfFiles(numOfFilesChan)

	maxColumns := <-maxColumnsChan
	numOfFiles := <-numOfFilesChan

	go getLastRowCount(&numOfFiles, &maxColumns, lastRowCountChan)
	go countRows(&maxColumns, &numOfFiles, numOfRowsChan)

	lastRowCount := <-lastRowCountChan
	numOfRows := <-numOfRowsChan
	printOrder := getTopToBottomOrder(&maxColumns, &numOfFiles, numOfRows, lastRowCount)

	return maxColumns, numOfRows, lastRowCount, printOrder
}

// Prints a file on the screen and determines when it is time to print a newline.
func printTopToBottomFile(currentColumn, maxColumns *int, file string) {
	if *currentColumn == *maxColumns {
		fmt.Println(file)
		*currentColumn = 0
	} else {
		fmt.Print(file)
	}
	*currentColumn++
}

// Reset the terminal at the end and add an extra newline if needed.
func resetTerminal(lastRowCount *int) {
	if *lastRowCount == 0 {
		fmt.Print(RESET)
	} else {
		fmt.Println(RESET)
	}
	
}

// Obtains statistics on the files to be printed, checks if printing order should be reversed,
// and finally prints files based on the printing order.
func printTopToBottom(colorizedList []string) {
	maxColumns, numOfRows, lastRowCount, printOrder := getPrintTopToBottomData()
	currentColumn := 1

	if *reversed {
		// Print all but the last row in descending order
		for index := ((numOfRows - 1) * maxColumns) - 1; index >= 0; index-- {
			printTopToBottomFile(&currentColumn, &maxColumns, colorizedList[printOrder[index]])
		}
		// Print the final row
		index := len(printOrder) - 1
		for count := 0; count < lastRowCount; count++ {
			fmt.Print(colorizedList[printOrder[index]])
			index--
		}
	} else {
		// Print files from top to bottom in ascending order
		for _, index := range printOrder {
			printTopToBottomFile(&currentColumn, &maxColumns, colorizedList[index])
		}
	}
	resetTerminal(&lastRowCount)
}

// This switch will determine how we should print.
func printSwitch() {
	switch {
	case *longMode:
		longModePrinter()
	case *singleColumn:
		singleColumnPrinter()
	case printOneLine:
		oneLinePrinter()
	default:
		printTopToBottom(getColorizedList())
	}
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU() + 1)
	processFlags()  // Process flags and arguments
	scanDirectory() // Load the directory list
	getFileStats()  // Obtain lists of file information
	printSwitch()   // Now that statistics have been gathered, it's time to process and print them.
}

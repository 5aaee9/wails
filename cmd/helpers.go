package cmd

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"time"

	"github.com/leaanthony/slicer"
	"github.com/leaanthony/spinner"
)

// ValidateFrontendConfig checks if the frontend config is valid
func ValidateFrontendConfig(projectOptions *ProjectOptions) error {
	if projectOptions.FrontEnd.Dir == "" {
		return fmt.Errorf("Frontend directory not set in project.json")
	}
	if projectOptions.FrontEnd.Build == "" {
		return fmt.Errorf("Frontend build command not set in project.json")
	}
	if projectOptions.FrontEnd.Install == "" {
		return fmt.Errorf("Frontend install command not set in project.json")
	}
	if projectOptions.FrontEnd.Bridge == "" {
		return fmt.Errorf("Frontend bridge config not set in project.json")
	}

	return nil
}

// InstallGoDependencies will run go get in the current directory
func InstallGoDependencies() error {
	depSpinner := spinner.New("Installing Dependencies...")
	depSpinner.SetSpinSpeed(50)
	depSpinner.Start()
	err := NewProgramHelper().RunCommand("go get")
	if err != nil {
		depSpinner.Error()
		return err
	}
	depSpinner.Success()
	return nil
}

// BuildApplication will attempt to build the project based on the given inputs
func BuildApplication(binaryName string, forceRebuild bool, buildMode string) error {
	compileMessage := "Packing + Compiling project"

	if buildMode == BuildModeDebug {
		compileMessage += " (Debug Mode)"
	}

	packSpinner := spinner.New(compileMessage + "...")
	packSpinner.SetSpinSpeed(50)
	packSpinner.Start()

	buildCommand := slicer.String()
	buildCommand.AddSlice([]string{"mewn", "build"})

	if binaryName != "" {
		buildCommand.Add("-o")
		buildCommand.Add(binaryName)
	}

	// If we are forcing a rebuild
	if forceRebuild {
		buildCommand.Add("-a")
	}

	// Setup ld flags
	ldflags := "-w -s "
	if buildMode == BuildModeDebug {
		ldflags = ""
	}
	ldflags += "-X github.com/wailsapp/wails.BuildMode=" + buildMode

	buildCommand.AddSlice([]string{"-ldflags", ldflags})
	err := NewProgramHelper().RunCommandArray(buildCommand.AsSlice())
	if err != nil {
		packSpinner.Error()
		return err
	}
	packSpinner.Success()
	return nil
}

// PackageApplication will attempt to package the application in a pltform dependent way
func PackageApplication(projectOptions *ProjectOptions) error {
	// Package app
	packageSpinner := spinner.New("Packaging Application")
	packageSpinner.SetSpinSpeed(50)
	packageSpinner.Start()
	err := NewPackageHelper().Package(projectOptions)
	if err != nil {
		packageSpinner.Error()
		return err
	}
	packageSpinner.Success()
	return nil
}

// BuildFrontend runs the given build command
func BuildFrontend(buildCommand string) error {
	buildFESpinner := spinner.New("Building frontend...")
	buildFESpinner.SetSpinSpeed(50)
	buildFESpinner.Start()
	err := NewProgramHelper().RunCommand(buildCommand)
	if err != nil {
		buildFESpinner.Error()
		return err
	}
	buildFESpinner.Success()
	return nil
}

// CheckMewn checks if mewn is installed and if not, attempts to fetch it
func CheckMewn() (err error) {
	programHelper := NewProgramHelper()
	if !programHelper.IsInstalled("mewn") {
		buildSpinner := spinner.New()
		buildSpinner.SetSpinSpeed(50)
		buildSpinner.Start("Installing Mewn asset packer...")
		err := programHelper.InstallGoPackage("github.com/leaanthony/mewn/cmd/mewn")
		if err != nil {
			buildSpinner.Error()
			return err
		}
		buildSpinner.Success()
	}
	return nil
}

// InstallFrontendDeps attempts to install the frontend dependencies based on the given options
func InstallFrontendDeps(projectDir string, projectOptions *ProjectOptions, forceRebuild bool, caller string) error {

	// Install frontend deps
	err := os.Chdir(projectOptions.FrontEnd.Dir)
	if err != nil {
		return err
	}

	// Check if frontend deps have been updated
	feSpinner := spinner.New("Installing frontend dependencies (This may take a while)...")
	feSpinner.SetSpinSpeed(50)
	feSpinner.Start()

	requiresNPMInstall := true

	// Read in package.json MD5
	fs := NewFSHelper()
	packageJSONMD5, err := fs.FileMD5("package.json")
	if err != nil {
		return err
	}

	const md5sumFile = "package.json.md5"

	// If we aren't forcing the install and the md5sum file exists
	if !forceRebuild && fs.FileExists(md5sumFile) {
		// Yes - read contents
		savedMD5sum, err := fs.LoadAsString(md5sumFile)
		// File exists
		if err == nil {
			// Compare md5
			if savedMD5sum == packageJSONMD5 {
				// Same - no need for reinstall
				requiresNPMInstall = false
				feSpinner.Success("Skipped frontend dependencies (-f to force rebuild)")
			}
		}
	}

	// Md5 sum package.json
	// Different? Build
	if requiresNPMInstall || forceRebuild {
		// Install dependencies
		err = NewProgramHelper().RunCommand(projectOptions.FrontEnd.Install)
		if err != nil {
			feSpinner.Error()
			return err
		}
		feSpinner.Success()

		// Update md5sum file
		ioutil.WriteFile(md5sumFile, []byte(packageJSONMD5), 0644)
	}

	bridgeFile := "wailsbridge.prod.js"
	if caller == "serve" {
		bridgeFile = "wailsbridge.js"
	}

	// Copy bridge to project
	_, filename, _, _ := runtime.Caller(1)
	bridgeFileSource := filepath.Join(path.Dir(filename), "..", "..", "wailsruntimeassets", "bridge", bridgeFile)
	bridgeFileTarget := filepath.Join(projectDir, projectOptions.FrontEnd.Dir, projectOptions.FrontEnd.Bridge, "wailsbridge.js")
	err = fs.CopyFile(bridgeFileSource, bridgeFileTarget)
	if err != nil {
		return err
	}

	// Build frontend
	err = BuildFrontend(projectOptions.FrontEnd.Build)
	if err != nil {
		return err
	}
	return nil
}

// ServeProject attempts to serve up the current project so that it may be connected to
// via the Wails bridge
func ServeProject(projectOptions *ProjectOptions, logger *Logger) error {
	go func() {
		time.Sleep(2 * time.Second)
		logger.Green(">>>>> To connect, you will need to run '" + projectOptions.FrontEnd.Serve + "' in the '" + projectOptions.FrontEnd.Dir + "' directory <<<<<")
	}()
	location, err := filepath.Abs(projectOptions.BinaryName)
	if err != nil {
		return err
	}

	logger.Yellow("Serving Application: " + location)
	cmd := exec.Command(location)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return err
	}

	return nil
}

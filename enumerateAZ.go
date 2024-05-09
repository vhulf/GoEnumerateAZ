package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	// "golang.org/x/crypto/ssh/terminal"
)

type GroupInfo struct {
	subscripton string
	name        string
}

type TemplateInfo struct { // Template is a specific kind of resource... useful for gathering all auto generated templates from deployments!
	subscription string
	group        string
	deployment   string
}

// type ResourceInfo struct { // Generic Resource for UPI and SPI
// 	subscription string
// 	group        string
// 	name         string
// 	resourceType string
// }

type ManagedIdentityInfo struct {
	subscription string
	fullScope    string
	principal    string
}

func check(e error) {
	if e != nil {
		panic(e)
	}
}

func verbPrint(toPrint string) {
	if verbose {
		fmt.Println("     ! " + toPrint)
	}
}

// * GLOBALVARS * //
var bearer string
var verbose bool

// ** AZ HELPER FUNCTIONS / SINGLE OPERATIONS **  //
func getSubscriptionList() []string {
	// go grab all the subscriptions...
	fmt.Println("    Grabbing all subscriptions...")
	subsReq, subsErr := http.NewRequest("GET", "https://management.azure.com/subscriptions?api-version=2020-06-01", nil)
	check(subsErr)
	subsReq.Header.Add("Authorization", "Bearer "+bearer)

	subsClient := &http.Client{}
	subsResp, err := subsClient.Do(subsReq)
	check(err)

	if subsResp.StatusCode == 401 {
		panic("Auth appears expired!")
	}

	subsBody, bodyErr := io.ReadAll(subsResp.Body)
	check(bodyErr)
	workingSubString := string(subsBody)
	defer subsResp.Body.Close()

	list := []string{}

	prelimiter := `"subscriptionId":"`
	for strings.Contains(workingSubString, prelimiter) {
		indexOfId := strings.Index(workingSubString, prelimiter) + len(prelimiter) // 18 is length of `"subscriptionId":"`
		list = append(list, workingSubString[indexOfId:indexOfId+36])              // 36 chars long UUID
		workingSubString = workingSubString[indexOfId+36:]
	}
	fmt.Println("    Success!")

	return list
}

func getGroupNamesOfSubscription(subscriptionId string) []GroupInfo {
	list := []GroupInfo{}
	groupReq, subsErr := http.NewRequest("GET", "https://management.azure.com/subscriptions/"+subscriptionId+"/resourcegroups?api-version=2020-06-01", nil)
	check(subsErr)
	groupReq.Header.Add("Authorization", "Bearer "+bearer)

	groupClient := &http.Client{}
	groupResp, err := groupClient.Do(groupReq)
	check(err)

	if groupResp.StatusCode == 401 {
		panic("Auth appears expired!")
	}

	groupBody, groupErr := io.ReadAll(groupResp.Body)
	check(groupErr)
	workingGroupString := string(groupBody)
	defer groupResp.Body.Close()

	prelimiter := `/resourceGroups/`
	postlimiter := `","name"`
	for strings.Contains(workingGroupString, prelimiter) {
		indexOfStringStart := strings.Index(workingGroupString, prelimiter) + len(prelimiter)
		indexOfDelimiter := strings.Index(workingGroupString, postlimiter)

		list = append(list, GroupInfo{subscriptionId, workingGroupString[indexOfStringStart:indexOfDelimiter]})
		workingGroupString = workingGroupString[indexOfDelimiter+(len(postlimiter)):]
	}

	return list
}

func getDeploymentsFromGroupName(group GroupInfo) []TemplateInfo {
	list := []TemplateInfo{}
	deployReq, subsErr := http.NewRequest("GET", "https://management.azure.com/subscriptions/"+group.subscripton+"/resourcegroups/"+group.name+"/deployments?api-version=2020-06-01", nil)
	check(subsErr)
	deployReq.Header.Add("Authorization", "Bearer "+bearer)

	deployClient := &http.Client{}
	deployResp, err := deployClient.Do(deployReq)
	check(err)

	if deployResp.StatusCode == 401 {
		panic("Auth appears expired!")
	}

	deployBody, deployErr := io.ReadAll(deployResp.Body)
	check(deployErr)
	workingDeployString := string(deployBody)
	defer deployResp.Body.Close()

	prelimiter := `/deployments/`
	postlimiter := `","name"`
	for strings.Contains(workingDeployString, prelimiter) {
		indexOfStringStart := strings.Index(workingDeployString, prelimiter) + len(prelimiter)
		indexOfPostlimiter := strings.Index(workingDeployString, postlimiter)

		if indexOfPostlimiter > indexOfStringStart {
			list = append(list, TemplateInfo{group.subscripton, group.name, workingDeployString[indexOfStringStart:indexOfPostlimiter]})
			workingDeployString = workingDeployString[indexOfPostlimiter+(len(postlimiter)):]
		} else {
			workingDeployString = ""
		}
	}

	return list
}

func grabTemplateAndWriteToFile(template TemplateInfo, file io.Writer) {
	body := []byte(`{"requests":[{"httpMethod":"GET","requestHeaderDetails":{"commandName":"HubsExtension.DeploymentOutputsBlade.fetchDeployment"},"url":"/subscriptions/` + template.subscription + `/resourceGroups/` + template.group + `/providers/Microsoft.Resources/deployments/` + template.deployment + `?api-version=2020-06-01"}]}`)

	req, err := http.NewRequest("POST", "https://management.azure.com/batch?api-version=2020-06-01", bytes.NewBuffer(body))
	check(err)
	req.Header.Add("Authorization", "Bearer "+bearer)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err1 := client.Do(req)
	check(err1)

	if resp.StatusCode == 401 {
		panic("Auth appears expired!")
	}

	body, err2 := io.ReadAll(resp.Body)
	check(err2)

	defer resp.Body.Close()

	buffer := bufio.NewWriter(file)
	entryHeader := ">TEMPLATE< --- " + template.subscription + " -> " + template.group + " -> " + template.deployment + "\n\n"
	buffer.WriteString(entryHeader)
	buffer.WriteString(string(body))
	buffer.WriteString("\n\n\n")

	buffer.Flush()
}

func getResourceScopesFromGroupName(group GroupInfo, nextLink string) []string {
	list := []string{}
	mappy := make(map[string]int)
	url := ""
	if nextLink == "" {
		url = "https://management.azure.com/subscriptions/" + group.subscripton + "/resourcegroups/" + group.name + "/resources?api-version=2020-06-01"
	} else {
		verbPrint("I've recursed, here's my nextLink: " + nextLink)
		url = nextLink
		nextLink = ""
	}

	resourceReq, resErr := http.NewRequest("GET", url, nil)
	check(resErr)
	resourceReq.Header.Add("Authorization", "Bearer "+bearer)

	resourceClient := &http.Client{}
	resourceResp, err := resourceClient.Do(resourceReq)
	check(err)

	if resourceResp.StatusCode == 401 {
		panic("Auth appears expired!")
	}

	resourceBody, resourceErr := io.ReadAll(resourceResp.Body)
	check(resourceErr)
	workingResourceString := string(resourceBody)
	defer resourceResp.Body.Close()

	if strings.Contains(workingResourceString, "nextLink") {
		nextLink = workingResourceString[strings.Index(workingResourceString, `nextLink":"`)+len(`nextLink":"`) : strings.LastIndex(workingResourceString, `"`)]
	}

	prelimiter := `"id":"`
	postlimiter := `","`
	for strings.Contains(workingResourceString, prelimiter) {
		workingResourceString = workingResourceString[strings.Index(workingResourceString, prelimiter):]
		indexOfStringStart := strings.Index(workingResourceString, prelimiter) + len(prelimiter)
		indexOfPostlimiter := strings.Index(workingResourceString, postlimiter)

		if indexOfPostlimiter > indexOfStringStart {
			scopie := workingResourceString[indexOfStringStart:indexOfPostlimiter]
			for strings.Count(scopie, "/") > 8 {
				scopie = scopie[:strings.LastIndex(scopie, "/")]
			}
			mappy[scopie] = 0
			workingResourceString = workingResourceString[indexOfPostlimiter+(len(postlimiter)):]
		} else {
			workingResourceString = ""
		}
	}
	for scope := range mappy {
		list = append(list, scope)
	}

	// TODO implement possible recursion for all gets D:
	if nextLink != "" {
		list = append(list, getResourceScopesFromGroupName(GroupInfo{"", ""}, nextLink)...)
	}

	return list
}

func getPrincipalFromResourceScope(resScope string) []ManagedIdentityInfo {
	list := []ManagedIdentityInfo{}
	subscription := resScope[strings.Index(resScope, "/subscriptions/")+len("/subscriptions/") : strings.Index(resScope, "/resourceGroups/")]
	prinReq, prinErr := http.NewRequest("GET", "https://management.azure.com"+resScope+"/providers/Microsoft.ManagedIdentity/identities/default?api-version=2023-01-31", nil)
	check(prinErr)
	prinReq.Header.Add("Authorization", "Bearer "+bearer)

	prinClient := &http.Client{}
	prinResp, err := prinClient.Do(prinReq)
	if err != nil {
		verbPrint("RESP ERROR:")
		verbPrint(err.Error())
		return []ManagedIdentityInfo{{subscription, resScope, "NO IDENTITIES FOUND, OR GLOBAL, OR OTHER ERROR..."}}
	}

	if prinResp.StatusCode == 401 {
		panic("Auth appears expired!")
	}

	prinBody, prinErr := io.ReadAll(prinResp.Body)
	check(prinErr)

	if prinResp.StatusCode == 400 {
		// we gotta snag an API version this resource supports and retry!
		if strings.Contains(resScope, "The supported api-versions are '") {
			newApiVersion := string(prinBody)[strings.Index(resScope, "The supported api-versions are '")+len("The supported api-versions are '") : strings.Index(resScope, ",")]
			fmt.Println("     !" + newApiVersion)
			prinReq, resErr := http.NewRequest("GET", "https://management.azure.com/"+resScope+"/providers/Microsoft.ManagedIdentity/identities/default?api-version="+newApiVersion, nil)
			check(resErr)
			prinReq.Header.Add("Authorization", "Bearer "+bearer)

			prinClient := &http.Client{}
			prinResp, err = prinClient.Do(prinReq)
			check(err)

			if prinResp.StatusCode == 401 {
				panic("Auth appears expired!")
			}

			prinBody, prinErr = io.ReadAll(prinResp.Body)
			check(prinErr)
		} else {
			verbPrint("400 BUT NO API VERSIONS : " + resScope)
			return []ManagedIdentityInfo{{subscription, resScope, "GLOBAL, OR OTHER ERROR..."}}
		}
	} else if prinResp.StatusCode == 404 {
		verbPrint("404... POSSIBLY EXPECTED! Heres my resScope: " + resScope)
		return []ManagedIdentityInfo{{subscription, resScope, "NO IDENTITIES FOUND..."}}
	} else if prinResp.StatusCode == 429 {
		verbPrint("429, we gotta chill out and retry!")
		time.Sleep(3)
		list = append(list, getPrincipalFromResourceScope(resScope)...)
	}

	verbPrint("HTTP Code must be good! : " + string(prinResp.Status) + " : " + resScope)

	// fmt.Println(string(prinBody))

	workingPrinString := string(prinBody)
	defer prinResp.Body.Close()

	prelimiter := `"principalId":"`
	postlimiter := `","clientId"`
	for strings.Contains(workingPrinString, prelimiter) {
		indexOfStringStart := strings.Index(workingPrinString, prelimiter) + len(prelimiter)
		indexOfPostlimiter := strings.Index(workingPrinString, postlimiter)

		//fmt.Println(string(indexOfStringStart) + string(indexOfPostlimiter))

		if indexOfPostlimiter > indexOfStringStart {
			list = append(list, ManagedIdentityInfo{subscription, resScope, workingPrinString[indexOfStringStart:indexOfPostlimiter]})
			verbPrint("Added identity principal: " + workingPrinString[indexOfStringStart:indexOfPostlimiter])
			workingPrinString = workingPrinString[indexOfPostlimiter+(len(postlimiter)):]
		} else {
			workingPrinString = ""
		}
	}

	return list
}

func grabRoleAssignmentsAndWriteToFile(identity ManagedIdentityInfo, file io.Writer) {
	// SKIP ANY PRINCIPAL ATTEMPTS THAT RETURNED EMPTY!
	if strings.Contains(identity.principal, " ") {
		return
	}

	req, err := http.NewRequest("GET", "https://management.azure.com/subscriptions/"+identity.subscription+"/providers/Microsoft.Authorization/roleAssignments?api-version=2020-04-01-preview&%24filter=assignedTo(%27"+identity.principal+"%27)", nil)
	check(err)
	req.Header.Add("Authorization", "Bearer "+bearer)

	client := &http.Client{}
	resp, err1 := client.Do(req)
	check(err1)

	if resp.StatusCode == 401 {
		panic("Auth appears expired!")
	}

	body, err2 := io.ReadAll(resp.Body)
	check(err2)

	defer resp.Body.Close()

	buffer := bufio.NewWriter(file)
	entryHeader := ">ROLE< --- " + identity.fullScope + " -> " + identity.principal + "\n\n"
	buffer.WriteString(entryHeader)
	// if string of body is less than 15, we can put No Custom Role Assignments Found
	if len(string(body)) <= 15 {
		buffer.WriteString("NO CUSTOM ROLE ASSIGNMENTS FOUND...")
	} else {
		buffer.WriteString(string(body) + "\n\n")
		buffer.WriteString(getRoleAssignmentSpecifics(string(body)))
	}
	buffer.WriteString("\n\n\n")

	buffer.Flush()
}

func getRoleAssignmentSpecifics(fullDef string) string {

	urlEnd := fullDef[strings.Index(fullDef, `"roleDefinitionId":"`)+len(`"roleDefinitionId":"`) : strings.LastIndex(fullDef, `","principalId"`)]
	urlEnd = urlEnd[strings.LastIndex(urlEnd, "/providers/"):]
	verbPrint("Final role definition url attempting! : " + urlEnd)

	req, err := http.NewRequest("GET", "https://management.azure.com"+urlEnd+"?api-version=2014-04-01-preview", nil)
	check(err)
	req.Header.Add("Authorization", "Bearer "+bearer)

	client := &http.Client{}
	resp, err1 := client.Do(req)
	check(err1)

	if resp.StatusCode == 401 {
		panic("Auth appears expired!")
	}

	body, err2 := io.ReadAll(resp.Body)
	check(err2)

	defer resp.Body.Close()

	return string(body)
}

// ** START ENUMERATION FUNCTIONS  **  //
func getDeploymentTemplates(outFile string) {
	fmt.Println("> Performing Deployment Template Enumeration...")

	fileHandler, err := os.Create(outFile)
	check(err)
	defer fileHandler.Close()

	templatesList := []TemplateInfo{}
	groupsList := []GroupInfo{}

	subscriptionsList := getSubscriptionList()
	if len(subscriptionsList) == 0 {
		fmt.Println("    Couldn't get subscriptions... was the token valid for scope 'management.azure.com' and is it alive?")
		os.Exit(0)
	}

	var waitGroups sync.WaitGroup
	waitGroups.Add(len(subscriptionsList))
	fmt.Println("    Grabbing all group names within subscriptions...")
	// go grab all the groupIds for ALL found subscriptions!
	for id := 0; id < len(subscriptionsList); id++ { // ","name":"
		go func(id int) {
			defer waitGroups.Done()

			groupsList = append(groupsList, getGroupNamesOfSubscription(subscriptionsList[id])...)
		}(id)
	}
	waitGroups.Wait()
	fmt.Println("    Success!")

	var waitDeployments sync.WaitGroup
	waitDeployments.Add(len(groupsList))
	fmt.Println("    Grabbing all deployments for each group name...")
	// go grab all the deployment information
	for i := 0; i < len(groupsList); i++ {
		go func(i int) {
			defer waitDeployments.Done()
			templatesList = append(templatesList, getDeploymentsFromGroupName(groupsList[i])...)
		}(i)

	}
	waitDeployments.Wait()
	fmt.Println("    Success!")
	fmt.Println("    Grabbing templates for all found sub/group/deployment groups and placing them into file!")

	var waitTemplates sync.WaitGroup
	waitTemplates.Add(len(templatesList))
	for ii := 0; ii < len(templatesList); ii++ {
		go func(ii int) {
			defer waitTemplates.Done()
			grabTemplateAndWriteToFile(templatesList[ii], fileHandler)
		}(ii)
	}
	waitTemplates.Wait()
	fileHandler.Sync()
	fmt.Println("    Success!")
	fmt.Println("> Final output was placed into '" + outFile + "'.")

}

func getManagedIdentityRoleAssignments(outFile string) {
	fmt.Println("> Performing User Managed Identity enumeration...")
	fileHandler, err := os.Create(outFile)
	check(err)
	defer fileHandler.Close()

	resourceList := []string{}
	groupsList := []GroupInfo{}
	principalsList := []ManagedIdentityInfo{}

	subscriptionsList := getSubscriptionList()

	var waitGroups sync.WaitGroup
	waitGroups.Add(len(subscriptionsList))
	fmt.Println("    Grabbing all group names within subscriptions...")
	// go grab all the groupIds for ALL found subscriptions!
	for id := 0; id < len(subscriptionsList); id++ { // ","name":"
		go func(id int) {
			defer waitGroups.Done()
			groupsList = append(groupsList, getGroupNamesOfSubscription(subscriptionsList[id])...)
		}(id)
	}
	waitGroups.Wait()
	fmt.Println("    Success!")

	var waitScopes sync.WaitGroup
	waitScopes.Add(len(groupsList))
	fmt.Println("    Grabbing all resource scope information within groups...")
	// get resources....
	for groupI := 0; groupI < len(groupsList); groupI++ { // ","name":"
		go func(groupI int) {
			defer waitScopes.Done()
			resourceList = append(resourceList, getResourceScopesFromGroupName(groupsList[groupI], "")...)
		}(groupI)
	}
	waitScopes.Wait()
	fmt.Println("    Success!")

	//fmt.Println(resourceList)

	var waitPrincipals sync.WaitGroup
	waitPrincipals.Add(len(resourceList))
	fmt.Println("    Grabbing principal identifiers for all resources...")
	// get UMIs "PrincipalIDs"  --- (may just be MIs of all kinds???)
	for resI := 0; resI < len(resourceList); resI++ { // ","name":"
		go func(resI int) {
			defer waitPrincipals.Done()
			principalsList = append(principalsList, getPrincipalFromResourceScope(resourceList[resI])...)
		}(resI)
	}
	waitPrincipals.Wait()
	fmt.Println("    Success!")

	var waitRoles sync.WaitGroup
	waitRoles.Add(len(principalsList))
	fmt.Println("    Grabbing role assignments for all principal identifiers...")
	// get UMIs "PrincipalIDs"  --- (may just be MIs of all kinds???)
	for prinI := 0; prinI < len(principalsList); prinI++ { // ","name":"
		go func(prinI int) {
			defer waitRoles.Done()
			grabRoleAssignmentsAndWriteToFile(principalsList[prinI], fileHandler)
		}(prinI)
	}
	waitRoles.Wait()
	fileHandler.Sync()
	fmt.Println("    Success!")
	fmt.Println("> Final output was placed into '" + outFile + "'.")

}

// ** MAIN **  //
func main() {
	// flags setup!
	var jwt string
	var useAzAuth bool
	var username string
	var getDeploymentTemplatesFlag bool
	var getManagedIdentityRoleAssignmentsFlag bool
	var customOutputFile string
	var verboseFlag bool

	flag.StringVar(&jwt, "jwt", "", "applicable JWT for selected operation (management API scope! , Can handle headers and 'Bearer', copy in however you please!)")
	flag.StringVar(&jwt, "t", "", "applicable JWT for selected operation (management API scope! , Can handle headers and 'Bearer', copy in however you please!)")
	flag.BoolVar(&useAzAuth, "azAuth", false, "get token from `az account get-access-token`")
	flag.BoolVar(&useAzAuth, "az", false, "get token from `az account get-access-token`")
	flag.StringVar(&username, "user", "", "get tokens as needed using username and password (will prompt for hidden password field) [NOT IMPLEMENTED]")
	flag.StringVar(&username, "u", "", "get tokens as needed using username and password (will prompt for hidden password field) [NOT IMPLEMENTED]")
	flag.BoolVar(&getDeploymentTemplatesFlag, "deploymentTemplates", false, "enable the enumeration of deployment templates (outputs: templatesOut.txt)")
	flag.BoolVar(&getDeploymentTemplatesFlag, "dt", false, "enable the enumeration of deployment templates (outputs: templatesOut.txt)")
	flag.BoolVar(&getManagedIdentityRoleAssignmentsFlag, "managedIdentityRoles", false, "enable the enumeration of user managed identities (outputs: UMIsOut.txt)")
	flag.BoolVar(&getManagedIdentityRoleAssignmentsFlag, "mir", false, "enable the enumeration of user managed identities (outputs: UMIsOut.txt)")
	// flag.BoolVar(&getSystemManagedIdentitiesFlag, "systemManagedIdentities", false, "enable the enumeration of system managed identities (outputs: SMIsOut.txt)")
	// flag.BoolVar(&getSystemManagedIdentitiesFlag, "smi", false, "enable the enumeration of system managed identities (outputs: SMIsOut.txt)")
	flag.StringVar(&customOutputFile, "outfile", "", "will set a singular output file instead of per-operation defaults")
	flag.StringVar(&customOutputFile, "o", "", "will set a singular output file instead of per-operation defaults")
	flag.BoolVar(&verboseFlag, "verbose", false, "enable more verbose debugging output")
	flag.BoolVar(&verboseFlag, "v", false, "enable more verbose debugging output")

	flag.Usage = func() {
		fmt.Printf("Usage of %s:\nKeep in mind at least one auth method is required, and that higher placed auth flags will cancel out lower ones if multiple are set.... \n", os.Args[0])

		//flag.PrintDefaults()
		flagSet := flag.CommandLine
		order := [][]string{
			[]string{"", "Authentication:"},
			[]string{"jwt", "t"},
			[]string{"azAuth", "az"},
			[]string{"user", "u"},
			[]string{"", "Enumeration:"},
			[]string{"deploymentTemplates", "dt"},
			[]string{"managedIdentityRoles", "mir"},
			// []string{"systemManagedIdentities", "smi"},
			[]string{"", "Optionals:"},
			[]string{"outfile", "o"},
			[]string{"verbose", "v"}}

		for _, set := range order {
			if set[0] == "" {
				fmt.Println("\n  " + set[1])
				continue
			}
			flag := flagSet.Lookup(set[0])
			fmt.Println("    --" + set[0] + " (-" + set[1] + ")  ->  " + flag.Usage)
		}

		fmt.Printf("\nEnumeration flags can be set in tandem for multi-operation running & output... \n")
	}

	flag.Parse()

	if verboseFlag {
		verbose = true
	} else {
		verbose = false
	}

	if len(jwt) != 0 {
		bearer = jwt

		if strings.Contains(bearer, "Bearer ") {
			bearer = bearer[strings.Index(bearer, "Bearer ")+7:]
		}
	} else if useAzAuth {
		out, err := exec.Command("az", "account", "get-access-token").Output()
		if err != nil {
			log.Fatal(err)
		}
		bearer = string(out)

		if strings.Contains(bearer, "Bearer ") {
			bearer = bearer[strings.Index(bearer, "Bearer ")+7:]
		}
	} else if len(username) != 0 {
		// fmt.Print("AZ Password: ")
		// passwd, err := terminal.ReadPassword(int(syscall.Stdin))
		// check(err)

		// //passwd = "NOTNEEDEDANYMORE"
		// passwd = []byte("NOTYETIMPLEMENTED")
		// fmt.Println(passwd)
	} else {
		bearer = "!NO-AUTH-SET_NO-AUTH-SET_NO-AUTH-SET!"
	}

	if getDeploymentTemplatesFlag {
		if len(customOutputFile) != 0 {
			getDeploymentTemplates(customOutputFile)
		} else {
			getDeploymentTemplates("deploymentTemplates.txt")
		}
	}

	if getManagedIdentityRoleAssignmentsFlag {
		if len(customOutputFile) != 0 {
			getManagedIdentityRoleAssignments(customOutputFile)
		} else {
			getManagedIdentityRoleAssignments("managedIdentityRoles.txt")
		}
	}

}
